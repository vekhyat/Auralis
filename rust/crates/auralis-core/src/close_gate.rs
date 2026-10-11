//! Pure close-gate state machine.

use std::sync::Mutex;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CloseState {
    Cold,
    Ready,
    Saving,
    Approved,
}

impl CloseState {
    pub fn as_str(&self) -> &'static str {
        match self {
            CloseState::Cold => "cold",
            CloseState::Ready => "ready",
            CloseState::Saving => "saving",
            CloseState::Approved => "approved",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CloseAction {
    AllowClose,
    FlushBeforeClose,
    WaitForCloseFlush,
}

impl CloseAction {
    pub fn as_str(&self) -> &'static str {
        match self {
            CloseAction::AllowClose => "allow_close",
            CloseAction::FlushBeforeClose => "flush_before_close",
            CloseAction::WaitForCloseFlush => "wait_for_close_flush",
        }
    }
}

#[derive(Debug)]
pub struct CloseGate {
    state: Mutex<CloseState>,
}

impl Default for CloseGate {
    fn default() -> Self {
        Self::new()
    }
}

impl CloseGate {
    pub fn new() -> Self {
        Self {
            state: Mutex::new(CloseState::Cold),
        }
    }

    pub fn ready(&self) {
        let mut guard = self.state.lock().unwrap();
        if *guard == CloseState::Cold {
            *guard = CloseState::Ready;
        }
    }

    pub fn request(&self) -> CloseAction {
        let mut guard = self.state.lock().unwrap();
        match *guard {
            CloseState::Cold | CloseState::Approved => CloseAction::AllowClose,
            CloseState::Ready => {
                *guard = CloseState::Saving;
                CloseAction::FlushBeforeClose
            }
            _ => CloseAction::WaitForCloseFlush,
        }
    }

    pub fn complete(&self, saved: bool) -> bool {
        let mut guard = self.state.lock().unwrap();
        if *guard != CloseState::Saving {
            return false;
        }
        if saved {
            *guard = CloseState::Approved;
            true
        } else {
            *guard = CloseState::Ready;
            false
        }
    }

    pub fn state(&self) -> CloseState {
        *self.state.lock().unwrap()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_close_gate_lifecycle() {
        let gate = CloseGate::new();
        assert_eq!(gate.state(), CloseState::Cold);

        // Request before ready allows immediate close
        assert_eq!(gate.request(), CloseAction::AllowClose);

        // Mark ready
        gate.ready();
        assert_eq!(gate.state(), CloseState::Ready);

        // Second ready call is idempotent
        gate.ready();
        assert_eq!(gate.state(), CloseState::Ready);

        // First close request asks to flush
        assert_eq!(gate.request(), CloseAction::FlushBeforeClose);
        assert_eq!(gate.state(), CloseState::Saving);

        // Subsequent close requests while saving must wait
        assert_eq!(gate.request(), CloseAction::WaitForCloseFlush);

        // Failed flush reverts to ready
        assert!(!gate.complete(false));
        assert_eq!(gate.state(), CloseState::Ready);

        // Next request asks to flush again
        assert_eq!(gate.request(), CloseAction::FlushBeforeClose);
        assert_eq!(gate.state(), CloseState::Saving);

        // Successful flush approves close
        assert!(gate.complete(true));
        assert_eq!(gate.state(), CloseState::Approved);

        // Once approved, close is allowed
        assert_eq!(gate.request(), CloseAction::AllowClose);
        assert_eq!(gate.state(), CloseState::Approved);

        // Complete when not saving returns false
        assert!(!gate.complete(true));
    }

    #[test]
    fn test_close_waits_for_save_and_can_retry_after_failure() {
        let gate = CloseGate::new();
        // Startup without frontend should be closable
        assert_eq!(gate.request(), CloseAction::AllowClose);

        gate.ready();
        // Ready frontend did get a save request
        assert_eq!(gate.request(), CloseAction::FlushBeforeClose);

        // Duplicate close emitted another save -> wait for close flush
        assert_eq!(gate.request(), CloseAction::WaitForCloseFlush);

        // Failed save must NOT approve close
        assert!(!gate.complete(false));

        // Failed save can be retried with a new save request
        assert_eq!(gate.request(), CloseAction::FlushBeforeClose);

        // Confirmed save approves close
        assert!(gate.complete(true));

        // Saved state allows close
        assert_eq!(gate.request(), CloseAction::AllowClose);
    }

    #[test]
    fn test_close_cannot_be_approved_before_request() {
        let gate = CloseGate::new();
        // Unsolicited close complete before request is rejected
        assert!(!gate.complete(true));

        gate.ready();
        // Ready is not yet a close request, so complete is rejected
        assert!(!gate.complete(true));

        // When request arrives, returns FlushBeforeClose
        assert_eq!(gate.request(), CloseAction::FlushBeforeClose);
    }
}
