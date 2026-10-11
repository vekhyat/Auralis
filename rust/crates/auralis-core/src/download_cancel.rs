//! Download cancellation lifecycle (port of `backend/download_cancel.go`).
//!
//! Two lifetimes are tracked, as in Go:
//! - each [`DownloadLifecycle::begin_scope`] call owns a child token;
//! - provider HTTP observes the shared parent via
//!   [`DownloadLifecycle::active_download_context`].
//!
//! Finishing one download cancels only its child. Force-stop and shutdown
//! cancel the parent, which cancels every child and every request still sharing
//! it. The parent is cleared when the last scope for it finishes, so a later
//! request never observes a cancelled token it does not own.
//!
//! The app uses the process-wide instance through the free functions at the
//! bottom of this module (Go keeps this state in a package global). Tests build
//! their own [`DownloadLifecycle`] so they can run in parallel.

use std::collections::HashMap;
use std::future::Future;
use std::sync::{Condvar, LazyLock, Mutex, MutexGuard, OnceLock};
use std::time::Duration;

pub use tokio_util::sync::CancellationToken;

/// Port of Go's `ErrDownloadCancelled`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, thiserror::Error)]
#[error("download cancelled")]
pub struct DownloadCancelled;

type Hook = Box<dyn Fn() + Send + Sync>;

#[derive(Default)]
struct State {
    accepting: bool,
    parent: Option<CancellationToken>,
    /// Bumped whenever a new parent is created; stands in for Go's
    /// `downloadState.parent == parent` identity check.
    parent_generation: u64,
    scopes: HashMap<u64, CancellationToken>,
    next_scope_id: u64,
    stopping: bool,
    /// Go's `wg` for tracked `DownloadTrack` calls.
    tracked: usize,
}

pub struct DownloadLifecycle {
    state: Mutex<State>,
    tracked_drained: Condvar,
    /// Runs after a force stop (Go calls `CancelQueuedAndDownloadingItems` and
    /// `ResetDownloading` there; the download queue registers them here).
    force_stop_hook: OnceLock<Hook>,
}

impl Default for DownloadLifecycle {
    fn default() -> Self {
        Self::new()
    }
}

impl DownloadLifecycle {
    pub fn new() -> Self {
        Self {
            state: Mutex::new(State {
                accepting: true,
                ..State::default()
            }),
            tracked_drained: Condvar::new(),
            force_stop_hook: OnceLock::new(),
        }
    }

    fn lock(&self) -> MutexGuard<'_, State> {
        self.state.lock().unwrap_or_else(|e| e.into_inner())
    }

    /// Registers the queue reset run by [`Self::force_stop_active_downloads`].
    /// Returns `false` if a hook was already registered.
    pub fn set_force_stop_hook(&self, hook: impl Fn() + Send + Sync + 'static) -> bool {
        self.force_stop_hook.set(Box::new(hook)).is_ok()
    }

    /// Port of `BeginDownloadCancellationScope`. Dropping or releasing the
    /// returned scope is Go's `finish` function.
    pub fn begin_scope(&self) -> DownloadScope<'_> {
        let mut s = self.lock();
        if !s.accepting {
            return DownloadScope {
                lifecycle: self,
                token: cancelled_token(),
                registration: None,
            };
        }
        let parent_unusable = s.parent.as_ref().is_none_or(|p| p.is_cancelled());
        if parent_unusable || s.stopping {
            s.parent = Some(CancellationToken::new());
            s.parent_generation += 1;
            s.stopping = false;
        }
        let parent = s.parent.clone().unwrap_or_default();
        let child = parent.child_token();
        let id = s.next_scope_id;
        s.next_scope_id += 1;
        s.scopes.insert(id, child.clone());
        let generation = s.parent_generation;
        drop(s);
        DownloadScope {
            lifecycle: self,
            token: child,
            registration: Some((id, generation)),
        }
    }

    fn release_scope(&self, token: &CancellationToken, id: u64, generation: u64) {
        token.cancel();
        let mut s = self.lock();
        s.scopes.remove(&id);
        let mut cancel_parent = None;
        if s.parent_generation == generation && s.parent.is_some() && s.scopes.is_empty() {
            cancel_parent = s.parent.take();
            s.stopping = false;
        }
        drop(s);
        if let Some(parent) = cancel_parent {
            parent.cancel();
        }
    }

    /// Port of `ActiveDownloadContext`.
    pub fn active_download_context(&self) -> CancellationToken {
        let s = self.lock();
        if let Some(parent) = &s.parent {
            return parent.clone();
        }
        if !s.accepting || s.stopping {
            return cancelled_token();
        }
        // Go's context.Background(): never cancelled.
        CancellationToken::new()
    }

    /// Port of `ForceStopActiveDownloads`.
    pub fn force_stop_active_downloads(&self) {
        let mut s = self.lock();
        let cancel = s.parent.clone();
        if cancel.is_some() {
            s.stopping = true;
        }
        drop(s);
        if let Some(parent) = cancel {
            parent.cancel();
        }
        if let Some(hook) = self.force_stop_hook.get() {
            hook();
        }
    }

    /// Port of `IsDownloadForceStopRequested`.
    pub fn is_force_stop_requested(&self) -> bool {
        self.lock().stopping
    }

    /// Port of `CheckDownloadCancelled`.
    pub fn check_download_cancelled(&self) -> Result<(), DownloadCancelled> {
        if !self.lock().accepting {
            return Err(DownloadCancelled);
        }
        if self.active_download_context().is_cancelled() {
            return Err(DownloadCancelled);
        }
        Ok(())
    }

    /// Port of `SleepWithDownloadContext`.
    pub async fn sleep_with_download_context(
        &self,
        delay: Duration,
    ) -> Result<(), DownloadCancelled> {
        if delay.is_zero() {
            return self.check_download_cancelled();
        }
        let ctx = self.active_download_context();
        tokio::select! {
            _ = ctx.cancelled() => Err(DownloadCancelled),
            _ = tokio::time::sleep(delay) => Ok(()),
        }
    }

    /// Port of `WithDownloadContext`: runs `fut` tied to the active download so
    /// a stop interrupts it.
    pub async fn with_download_context<F: Future>(
        &self,
        fut: F,
    ) -> Result<F::Output, DownloadCancelled> {
        let ctx = self.active_download_context();
        tokio::select! {
            biased;
            _ = ctx.cancelled() => Err(DownloadCancelled),
            out = fut => Ok(out),
        }
    }

    /// The condition under which Go's `WrapDownloadCancelled` replaces `err`
    /// with `ErrDownloadCancelled`.
    pub fn should_wrap_as_cancelled(&self, err: &(dyn std::error::Error + 'static)) -> bool {
        self.is_force_stop_requested() || is_download_cancelled_error(err)
    }

    /// Port of `TryBeginTrackedDownload`. Dropping the guard is `EndTrackedDownload`.
    pub fn try_begin_tracked_download(&self) -> Result<TrackedDownload<'_>, DownloadCancelled> {
        let mut s = self.lock();
        if !s.accepting {
            return Err(DownloadCancelled);
        }
        s.tracked += 1;
        Ok(TrackedDownload { lifecycle: self })
    }

    /// Port of `StopAcceptingDownloadsAndDrain`: rejects new downloads, cancels
    /// the active parent and blocks until tracked downloads end. Call it from a
    /// blocking context (e.g. `spawn_blocking`).
    pub fn stop_accepting_downloads_and_drain(&self) {
        self.lock().accepting = false;
        self.force_stop_active_downloads();
        let mut s = self.lock();
        while s.tracked > 0 {
            s = self
                .tracked_drained
                .wait(s)
                .unwrap_or_else(|e| e.into_inner());
        }
    }
}

/// A download's own cancellation scope.
pub struct DownloadScope<'a> {
    lifecycle: &'a DownloadLifecycle,
    token: CancellationToken,
    registration: Option<(u64, u64)>,
}

impl DownloadScope<'_> {
    pub fn token(&self) -> &CancellationToken {
        &self.token
    }

    /// Go's `finish()`; also runs on drop. Idempotent like Go's `sync.Once`.
    pub fn release(&mut self) {
        if let Some((id, generation)) = self.registration.take() {
            self.lifecycle.release_scope(&self.token, id, generation);
        }
    }
}

impl Drop for DownloadScope<'_> {
    fn drop(&mut self) {
        self.release();
    }
}

/// Guard for a tracked download; dropping it is Go's `EndTrackedDownload`.
pub struct TrackedDownload<'a> {
    lifecycle: &'a DownloadLifecycle,
}

impl Drop for TrackedDownload<'_> {
    fn drop(&mut self) {
        let mut s = self.lifecycle.lock();
        s.tracked = s.tracked.saturating_sub(1);
        if s.tracked == 0 {
            self.lifecycle.tracked_drained.notify_all();
        }
    }
}

fn cancelled_token() -> CancellationToken {
    let token = CancellationToken::new();
    token.cancel();
    token
}

/// Port of `IsDownloadCancelledError`: true if `err` or any source is
/// [`DownloadCancelled`].
pub fn is_download_cancelled_error(err: &(dyn std::error::Error + 'static)) -> bool {
    let mut current = Some(err);
    while let Some(e) = current {
        if e.is::<DownloadCancelled>() {
            return true;
        }
        current = e.source();
    }
    false
}

static GLOBAL: LazyLock<DownloadLifecycle> = LazyLock::new(DownloadLifecycle::new);

/// The process-wide lifecycle (Go's package-level `downloadState`).
pub fn global() -> &'static DownloadLifecycle {
    &GLOBAL
}

pub fn begin_download_cancellation_scope() -> DownloadScope<'static> {
    GLOBAL.begin_scope()
}

pub fn active_download_context() -> CancellationToken {
    GLOBAL.active_download_context()
}

pub fn force_stop_active_downloads() {
    GLOBAL.force_stop_active_downloads()
}

pub fn is_download_force_stop_requested() -> bool {
    GLOBAL.is_force_stop_requested()
}

pub fn check_download_cancelled() -> Result<(), DownloadCancelled> {
    GLOBAL.check_download_cancelled()
}

pub async fn sleep_with_download_context(delay: Duration) -> Result<(), DownloadCancelled> {
    GLOBAL.sleep_with_download_context(delay).await
}

pub async fn with_download_context<F: Future>(fut: F) -> Result<F::Output, DownloadCancelled> {
    GLOBAL.with_download_context(fut).await
}

pub fn try_begin_tracked_download() -> Result<TrackedDownload<'static>, DownloadCancelled> {
    GLOBAL.try_begin_tracked_download()
}

pub fn stop_accepting_downloads_and_drain() {
    GLOBAL.stop_accepting_downloads_and_drain()
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicUsize, Ordering};
    use std::sync::Arc;

    #[test]
    fn begin_download_cancellation_scope_does_not_reuse_cancelled_context() {
        let lc = DownloadLifecycle::new();
        let mut a = lc.begin_scope();
        lc.force_stop_active_downloads();
        assert!(
            a.token().is_cancelled(),
            "force stop should cancel the in-flight scope"
        );

        let b = lc.begin_scope();
        assert!(
            !b.token().is_cancelled(),
            "new download reused a cancelled context"
        );

        a.release();
        assert!(
            !b.token().is_cancelled(),
            "finishing the old scope cancelled the new one"
        );
    }

    #[test]
    fn download_scope_finish_does_not_cancel_sibling() {
        let lc = DownloadLifecycle::new();
        let mut a = lc.begin_scope();
        let b = lc.begin_scope();

        a.release();
        assert!(
            a.token().is_cancelled(),
            "finished scope should cancel its own context"
        );
        assert!(
            !b.token().is_cancelled(),
            "finishing one download cancelled the other"
        );
        assert!(
            !lc.active_download_context().is_cancelled(),
            "provider context cancelled while another download is active"
        );
    }

    #[test]
    fn stop_accepting_downloads_cancels_and_rejects_new_work() {
        let lc: &'static DownloadLifecycle = Box::leak(Box::new(DownloadLifecycle::new()));
        let tracked = lc.try_begin_tracked_download().unwrap();
        let scope = lc.begin_scope();
        let (started_tx, started_rx) = std::sync::mpsc::channel();
        let (finished_tx, finished_rx) = std::sync::mpsc::channel();
        std::thread::spawn(move || {
            let _tracked = tracked;
            let scope = scope;
            started_tx.send(()).unwrap();
            while !scope.token().is_cancelled() {
                std::thread::sleep(Duration::from_millis(5));
            }
            drop(scope);
            drop(_tracked);
            let _ = finished_tx.send(());
        });
        started_rx.recv().unwrap();

        let (drained_tx, drained_rx) = std::sync::mpsc::channel();
        std::thread::spawn(move || {
            lc.stop_accepting_downloads_and_drain();
            let _ = drained_tx.send(());
        });

        finished_rx
            .recv_timeout(Duration::from_secs(2))
            .expect("in-flight download was not cancelled");
        drained_rx
            .recv_timeout(Duration::from_secs(2))
            .expect("shutdown did not finish draining");
        assert!(
            lc.try_begin_tracked_download().is_err(),
            "new download accepted after shutdown"
        );
    }

    #[test]
    fn last_scope_release_clears_parent() {
        let lc = DownloadLifecycle::new();
        let mut a = lc.begin_scope();
        let shared = lc.active_download_context();
        a.release();
        assert!(
            shared.is_cancelled(),
            "parent is cancelled with its last scope"
        );
        assert!(!lc.active_download_context().is_cancelled());
        assert!(lc.check_download_cancelled().is_ok());
    }

    #[test]
    fn rejected_scope_after_shutdown_is_already_cancelled() {
        let lc = DownloadLifecycle::new();
        lc.stop_accepting_downloads_and_drain();
        let scope = lc.begin_scope();
        assert!(scope.token().is_cancelled());
        assert_eq!(lc.check_download_cancelled(), Err(DownloadCancelled));
        assert!(lc.active_download_context().is_cancelled());
    }

    #[test]
    fn force_stop_runs_queue_hook() {
        let lc = DownloadLifecycle::new();
        let calls = Arc::new(AtomicUsize::new(0));
        let seen = calls.clone();
        assert!(lc.set_force_stop_hook(move || {
            seen.fetch_add(1, Ordering::SeqCst);
        }));
        lc.force_stop_active_downloads();
        assert_eq!(calls.load(Ordering::SeqCst), 1);
        assert!(
            !lc.is_force_stop_requested(),
            "no parent, so stopping is not set"
        );
        let _scope = lc.begin_scope();
        lc.force_stop_active_downloads();
        assert!(lc.is_force_stop_requested());
        assert_eq!(calls.load(Ordering::SeqCst), 2);
    }

    #[tokio::test]
    async fn sleep_and_request_observe_force_stop() {
        let lc: &'static DownloadLifecycle = Box::leak(Box::new(DownloadLifecycle::new()));
        let _scope = lc.begin_scope();
        let sleeper = tokio::spawn(lc.sleep_with_download_context(Duration::from_secs(30)));
        let request = tokio::spawn(lc.with_download_context(std::future::pending::<()>()));
        tokio::time::sleep(Duration::from_millis(20)).await;
        lc.force_stop_active_downloads();
        assert_eq!(sleeper.await.unwrap(), Err(DownloadCancelled));
        assert_eq!(request.await.unwrap(), Err(DownloadCancelled));
        assert_eq!(
            lc.sleep_with_download_context(Duration::ZERO).await,
            Err(DownloadCancelled)
        );
    }

    #[test]
    fn cancelled_error_is_found_in_source_chain() {
        #[derive(Debug, thiserror::Error)]
        #[error("wrapped")]
        struct Wrapped(#[source] DownloadCancelled);
        assert!(is_download_cancelled_error(&Wrapped(DownloadCancelled)));
        let other = std::io::Error::other("x");
        assert!(!is_download_cancelled_error(&other));
    }
}
