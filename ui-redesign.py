from pathlib import Path
import json, re

root = Path('frontend/src')
def edit(name, fn):
    p = root / name
    p.write_text(fn(p.read_text(encoding='utf-8')), encoding='utf-8')

# One download path for tracks, albums, playlists and selections.
def track(s):
    s = s.replace('ListPlus, ', '')
    s = s.replace('track, isDownloading, downloadingTrack,', 'track, downloadingTrack,').replace('skippedCover, onDownload, onQueueTrack,', 'skippedCover, onQueueTrack,')
    start = s.index('        <Button size="sm" onClick={() => onDownload(')
    end = s.index('        <Tooltip>', start)
    s = s[:start] + '''        <Button onClick={() => onQueueTrack?.(track)} disabled={trackQueued || !onQueueTrack}>
          {downloadingTrack === track.spotify_id ? <Spinner /> : trackQueued ? <CircleCheck className="size-4" /> : <Download className="size-4" />}
          {t(trackQueued ? "translation.downloads.requested" : "translation.trackInfo.download")}
        </Button>
''' + s[end:]
    s = s.replace('mx-auto w-full max-w-3xl', 'track-detail mx-auto w-full max-w-5xl')
    s = s.replace('flex items-start gap-6', 'flex items-start gap-8')
    s = s.replace('h-[120px] w-[120px] shrink-0 rounded-[2px]', 'h-[240px] w-[240px] shrink-0 rounded-xl')
    s = s.replace('text-lg leading-snug font-semibold break-words', 'text-3xl leading-tight font-semibold tracking-tight break-words')
    s = s.replace('<p className="text-[11px] font-semibold tracking-widest uppercase text-muted-foreground">{t("translation.artistInfo.track")}</p>', '')
    return s
edit('components/TrackInfo.tsx', track)

for name in ['AlbumInfo', 'PlaylistInfo', 'ArtistInfo', 'TrackList']:
    def collection(s):
        s = s.replace('ListPlus', 'Download')
        s = s.replace('translation.queue.addToQueue', 'translation.downloads.download').replace('translation.queue.alreadyInQueue', 'translation.downloads.requested').replace('translation.queue.addSelectedQueueValue1', 'translation.downloads.downloadSelected')
        if name != 'TrackList':
            s = s.replace('return (<div className="flex items-start gap-6">', 'return (<div className="collection-layout flex flex-col gap-7">')
        return s
    edit(f'components/{name}.tsx', collection)

def inspector(s):
    s = s.replace('import { ExternalLink }', 'import { CoverArt } from "./ArtworkCard";\nimport { ExternalLink }')
    start = s.index('    return (<aside')
    end = s.index('\n}\n', start)
    s = s[:start] + '''    return (<aside className={cn("inspector-pane w-72 shrink-0", className)}>
      <CoverArt src={cover} className="inspector-cover w-full" />
      <div className="inspector-body mt-5 min-w-0">
        <h2 className="text-2xl leading-tight font-semibold tracking-tight break-words">{title}</h2>
        <p className="mt-2 text-sm text-muted-foreground">{[eyebrow ?? fallbackMark, subtitle].filter(Boolean).map((part, index) => <span key={index}>{index > 0 && " · "}{part}</span>)}</p>
        {actions && <div className="my-5 flex flex-wrap items-center gap-2">{actions}</div>}
        {rows.length > 0 && <dl className="grid gap-x-6 border-t border-border">
          {rows.map((row) => <div key={row.label} className="flex items-baseline justify-between gap-4 border-b border-border py-2.5">
            <dt className="shrink-0 text-xs text-muted-foreground">{row.label}</dt>
            <dd className="min-w-0 text-right text-sm break-words">{row.value}</dd>
          </div>)}
        </dl>}
        {footer && <div className="mt-4 text-xs text-muted-foreground">{footer}</div>}
      </div>
    </aside>);''' + s[end:]
    return s
edit('components/InspectorPane.tsx', inspector)

def history(s):
    s = s.replace('import { CatalogRow } from "./CatalogRow";', 'import { ArtworkCard } from "./ArtworkCard";\nimport { X } from "lucide-react";')
    s = s.replace('text-[11px] font-semibold tracking-widest uppercase text-muted-foreground', 'text-xl font-semibold tracking-tight')
    s = s.replace('className="space-y-1"', 'className="space-y-5"').replace('className="border-t border-border"', 'className="artwork-grid grid grid-cols-2 gap-5 sm:grid-cols-3 xl:grid-cols-5"')
    s = s.replace('<CatalogRow', '<ArtworkCard')
    s = re.sub(r'\s+coverFallback=\{[^\n]+\}', '', s)
    s = re.sub(r'\s+explicit=\{item.is_explicit\}', '', s)
    s = s.replace('cursor-pointer px-2 py-1 text-[11px] text-muted-foreground transition-colors hover:text-destructive', 'absolute top-4 right-4 flex size-7 cursor-pointer items-center justify-center rounded-full border bg-background text-muted-foreground opacity-0 transition-opacity hover:text-destructive group-hover:opacity-100 group-focus-within:opacity-100')
    s = s.replace('{t("translation.catalog.remove")}\n          </button>', '<X className="size-3.5" />\n          </button>')
    return s
edit('components/FetchHistory.tsx', history)

def catalog(s):
    s = s.replace('import { CatalogRow } from "./CatalogRow";', 'import { ArtworkCard } from "./ArtworkCard";\nimport { Music2, ArrowUp } from "lucide-react";')
    s = s.replace('<p className="max-w-[70ch] text-sm text-muted-foreground">{t("translation.catalog.emptyHint")}</p>', '''<div className="library-intro mb-5 flex items-start justify-between gap-6">
        <div><h1 className="text-3xl font-semibold tracking-tight">{t("translation.downloads.libraryTitle")}</h1>
        <p className="mt-3 max-w-[60ch] text-sm leading-6 text-muted-foreground">{t("translation.downloads.libraryHint")}</p></div>
        <ArrowUp className="mt-2 hidden size-5 text-muted-foreground sm:block" />
      </div>
      {history.length === 0 && <div className="my-8 flex min-h-52 items-center gap-6 rounded-xl border border-dashed bg-card p-8">
        <Music2 className="size-12 shrink-0 text-primary" strokeWidth={1} />
        <div><h2 className="text-lg font-semibold">{t("translation.downloads.emptyLibrary")}</h2><p className="mt-2 max-w-[52ch] text-sm leading-6 text-muted-foreground">{t("translation.downloads.emptyLibraryHint")}</p></div>
      </div>}''')
    s = s.replace('return (<div className="flex items-start gap-6">', 'return (<div className="search-layout flex items-start gap-8">')
    s = s.replace('<div className="border-b border-border">\n          {resultsForTab', '<div className="artwork-grid mt-6 grid grid-cols-2 gap-4 sm:grid-cols-3 xl:grid-cols-4">\n          {resultsForTab')
    s = s.replace('<CatalogRow', '<ArtworkCard')
    s = re.sub(r'\s+coverFallback=\{[^\n]+\}', '', s)
    s = re.sub(r'\s+explicit=\{item.is_explicit\}', '', s)
    s = s.replace('className="hidden w-80 shrink-0 border-l border-border pl-6 lg:block"', 'className="hidden w-72 shrink-0 rounded-xl bg-secondary/50 p-6 lg:block"')
    return s
edit('components/CatalogPane.tsx', catalog)

def app(s):
    s = s.replace('import { DownloadProgressToast } from "@/components/DownloadProgressToast";', 'import { DownloadShelf } from "@/components/DownloadShelf";')
    s = s.replace('toast.success(t("translation.queue.addedValue1Queue", { value1: label }));', 'toast.success(t(queue.isSuspended ? "translation.downloads.addedPaused" : "translation.downloads.added", { name: label }), { action: { label: t("translation.downloads.title"), onClick: () => setCurrentPage("queue") } });')
    s = s.replace('}, [t]);\n    const handleQueueTracks', '}, [t, queue.isSuspended]);\n    const handleQueueTracks')
    s = s.replace('toast.success(t("translation.queue.addedValue1TracksQueue", { value1: result.added.toLocaleString() }));', 'reportQueueAdd(result, t("translation.downloads.trackCount", { count: result.added }));')
    s = s.replace('translation.queue.alreadyInQueue', 'translation.downloads.requested')
    s = s.replace('items={queue.items} isProcessing={queue.isProcessing}', 'items={queue.items} isSuspended={queue.isSuspended} onOpenLibrary={() => handlePageChange("main")} onOpenFolder={handleOpenFolder} isProcessing={queue.isProcessing}')
    s = s.replace('className="fixed inset-x-0 top-11 bottom-0 overflow-y-auto overflow-x-hidden"', 'className="app-content fixed right-0 top-16 bottom-[76px] left-[184px] overflow-y-auto overflow-x-hidden"')
    s = s.replace('className="px-6 py-5"', 'className="mx-auto max-w-[1600px] px-8 py-8"')
    s = s.replace('<DownloadProgressToast isPreparing={queue.isProcessing} onOpenQueue={() => handlePageChange("queue")}/>', '''<DownloadShelf items={queue.items} isProcessing={queue.isProcessing} isPausing={queue.isPausing} isSuspended={queue.isSuspended}
              onOpen={() => handlePageChange("queue")} onPause={() => queue.pause()} onResume={() => void queue.start()}
              onStop={() => queue.stop()} onFolder={handleOpenFolder} />''')
    return s
edit('App.tsx', app)

def titlebar(s):
    s = s.replace('    Bug,', '    Bug,\n    Library,\n    Download,\n    History,\n    Settings,')
    s = s.replace('queue: t("translation.queue.queue")', 'queue: t("translation.downloads.title")')
    s = s.replace('    const destinations =', '    const destinationIcons = { main: Library, queue: Download, history: History, settings: Settings };\n    const destinations =')
    s = s.replace('flex h-11 items-center gap-3 border-b bg-background pr-0 pl-3', 'flex h-16 items-center gap-4 border-b bg-card pr-0 pl-5')
    s = s.replace('flex shrink-0 items-center gap-2', 'flex w-[148px] shrink-0 items-center gap-2.5', 1)
    s = s.replace('text-[13px] font-semibold tracking-tight', 'text-lg font-semibold tracking-tight', 1)
    s = s.replace('ml-auto flex h-full shrink-0 items-stretch gap-0.5', 'shell-navigation fixed top-16 bottom-[76px] left-0 flex w-[184px] flex-col gap-1 border-r bg-card p-3 pt-6')
    s = s.replace('relative flex cursor-pointer items-center gap-1.5 px-2.5 text-[12.5px] transition-colors', 'relative flex h-11 cursor-pointer items-center gap-3 rounded-lg px-3 text-sm transition-colors')
    s = s.replace('font-semibold text-primary after:absolute after:inset-x-2.5 after:bottom-2 after:h-px after:bg-primary', 'bg-primary/10 font-semibold text-primary')
    s = s.replace('<span className="whitespace-nowrap">{destination.label}</span>', '<span aria-hidden="true">{(() => { const Icon = destinationIcons[destination.page]; return <Icon className="size-[18px]" />; })()}</span><span className="whitespace-nowrap">{destination.label}</span>')
    s = s.replace('font-mono text-[11px] tabular-nums opacity-80', 'ml-auto rounded bg-primary/10 px-1.5 text-xs tabular-nums')
    s = s.replace('border-none bg-transparent shadow-none px-0', 'mt-auto border-none bg-transparent shadow-none px-0')
    s = s.replace('className="flex h-full shrink-0 items-stretch"', 'className="ml-auto flex h-full shrink-0 items-stretch"')
    return s
edit('components/TitleBar.tsx', titlebar)

def queue(s):
    s = s.replace('    value: QueueItemType;', '    value: QueueItemType | "all";')
    s = s.replace('}> = [', '}> = [\n    { value: "all", label: "translation.queue.all" },', 1)
    s = s.replace('    items: QueueItem[];', '    items: QueueItem[];\n    isSuspended?: boolean;\n    onOpenLibrary?: () => void;\n    onOpenFolder?: () => void;')
    s = s.replace('export function QueuePage({ items,', 'export function QueuePage({ isSuspended = false, onOpenLibrary, onOpenFolder, items,')
    s = re.sub(r'const \[activeTab, setActiveTab\] = useState<QueueItemType>\([^\n]+', 'const [activeTab, setActiveTab] = useState<QueueItemType | "all">("all");', s)
    s = s.replace('const tabItems = items.filter((item) => item.type === activeTab);', 'const tabItems = activeTab === "all" ? items : items.filter((item) => item.type === activeTab);')
    s = s.replace('const handleTabChange = (value: QueueItemType)', 'const handleTabChange = (value: QueueItemType | "all")')
    s = s.replace('clearQueue(activeTab)', 'clearQueue(activeTab === "all" ? undefined : activeTab)').replace('clearFinishedQueueItems(activeTab)', 'clearFinishedQueueItems(activeTab === "all" ? undefined : activeTab)')
    s = s.replace('const count = items.filter((item) => item.type === tab.value).length;', 'const count = tab.value === "all" ? items.length : items.filter((item) => item.type === tab.value).length;')
    s = s.replace('<h1 className="text-lg font-semibold tracking-tight">{t("translation.queue.queue")}</h1>', '<div><h1 className="text-3xl font-semibold tracking-tight">{t("translation.downloads.title")}</h1><p className="mt-2 text-sm text-muted-foreground">{t(isSuspended ? "translation.downloads.pausedHint" : "translation.downloads.autoHint")}</p></div>')
    s = s.replace('t("translation.queue.stopAll")', 't("translation.downloads.cancelCurrent")')
    s = s.replace('{pausedCount > 0 ? t("translation.queue.resumeAll") : t("translation.queue.startAll")}', '{t("translation.queue.resumeAll")}')
    # Restored pending work has an explicit Resume control; fresh requests auto-start.
    s = s.replace(') : (<Button onClick={() => handleStart()}', ') : runnableCount > 0 ? (<Button onClick={() => onStart()}')
    s = s.replace('disabled={runnableCount === 0}', 'disabled={runnableCount === 0}')
    s = s.replace('</Button>)}\n                </div>\n            </div>', '</Button>) : null}\n                    {onOpenFolder && <Button variant="outline" onClick={onOpenFolder}>{t("translation.common.openFolder")}</Button>}\n                </div>\n            </div>', 1)
    start = s.index('                {tabRun ? (<>')
    end = s.index('\n            </div>', start)
    s = s[:start] + s[end:]
    # All downloads share one worker; no separate per-type start buttons.
    s = re.sub(r'    const handleStart = \(type\?: QueueItemType\) => \{.*?\n    \};', '', s, flags=re.S)
    s = re.sub(r'    const tabPendingCount =.*?\n', '', s)
    s = re.sub(r'    const tabPausedCount =.*?\n', '', s)
    s = re.sub(r'    const tabRunnableCount =.*?\n', '', s)
    s = re.sub(r'    const pausedCount =.*?\n', '', s)
    s = s.replace('const runnableCount = pendingCount + pausedCount;', 'const runnableCount = pendingCount + items.filter((item) => item.status === "paused").length;')
    s = re.sub(r'    const tabRun =.*?\n', '', s)
    s = s.replace(', showsTabQueueControls', '')
    s = s.replace('translation.queue.emptyQueue', 'translation.downloads.empty').replace('translation.queue.addToQueueHint', 'translation.downloads.emptyHint')
    s = s.replace('<ListOrdered className="size-9 opacity-30"/>', '<ListOrdered className="size-9 opacity-30"/>\n                        {onOpenLibrary && <Button variant="outline" onClick={onOpenLibrary}>{t("translation.downloads.findMusic")}</Button>}')
    s = s.replace('size-7 shrink-0 overflow-hidden rounded-[2px]', 'size-12 shrink-0 overflow-hidden rounded-lg')
    s = s.replace('translation.queue.start', 'translation.queue.resume')
    # Retry notification now starts the worker itself.
    s = s.replace('handleStart(activeTab)', 'onStart(activeTab === "all" ? undefined : activeTab)')
    return s
edit('components/QueuePage.tsx', queue)

copy = {
    'title': 'Downloads', 'download': 'Download', 'downloadSelected': 'Download selected ({{value1}})',
    'requested': 'In downloads', 'added': '{{name}} added to downloads', 'addedPaused': '{{name}} added · downloads are paused',
    'trackCount': '{{count}} tracks', 'ready': 'Ready when you are', 'autoHint': 'Downloads start automatically. Keep exploring while we take care of the files.',
    'downloading': 'Downloading', 'waitingResume': 'Waiting for you to resume', 'cancelCurrent': 'Cancel current download',
    'pausedHint': 'Downloads are paused. New requests will wait until you resume.',
    'empty': 'Your next download starts here', 'emptyHint': 'Find a track, album, or playlist in your library and choose Download.',
    'findMusic': 'Find music', 'libraryTitle': 'Your music, ready to keep.',
    'libraryHint': 'Find a favorite. Explore the artwork and track list. Download it in one click.',
    'emptyLibrary': 'Start with something you love', 'emptyLibraryHint': 'Paste a Spotify link or search for an artist, album, or track above. Your recently explored music will appear here.'
}
for p in (root / 'locales').glob('*.json'):
    data = json.loads(p.read_text(encoding='utf-8'))
    translation = data.get('translation', data)
    translation['downloads'] = copy
    if p.name == 'en.json':
        translation['catalog']['fetchLossless'] = 'View tracks & details'
        translation['queue']['pending'] = 'Waiting'
        translation['queue']['openQueue'] = 'Open downloads'
        translation['queue']['searchQueue'] = 'Search downloads…'
    p.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')

# Keep the proven palette; artwork and hierarchy now carry the experience.
edit('index.css', lambda s: s.replace('--radius-sm: 0px;', '--radius-sm: 4px;').replace('--radius-md: 2px;', '--radius-md: 6px;').replace('--radius-lg: 2px;', '--radius-lg: 8px;').replace('--radius-xl: 2px;', '--radius-xl: 12px;') + '''

/* Artwork library: spacious browsing, compact track lists, persistent transfers. */
.collection-layout > .inspector-pane {
  order: -1;
  display: grid;
  grid-template-columns: 208px minmax(0, 1fr);
  gap: 32px;
  width: 100%;
  padding-bottom: 28px;
  border-bottom: 1px solid var(--border);
}
.collection-layout > .inspector-pane .inspector-body { margin-top: 0; }
.collection-layout > .inspector-pane h2 { font-size: 32px; line-height: 1.15; }
.collection-layout > .inspector-pane dl { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.collection-layout > div { width: 100%; }
.artwork-card button:focus-visible { outline: 2px solid var(--primary); outline-offset: 5px; border-radius: 6px; }
button, input { caret-color: var(--primary); }
.app-content { scrollbar-gutter: stable; }
.download-shelf button { min-height: 36px; }
@media (max-width: 1100px) {
  .shell-navigation { width: 152px; padding-inline: 8px; }
  .app-content { left: 152px; }
  .search-layout { flex-direction: column; }
  .search-layout > div { width: 100%; }
  .search-layout .inspector-pane { display: grid; grid-template-columns: 140px 1fr; gap: 24px; width: 100%; }
  .search-layout .inspector-body { margin-top: 0; }
}
@media (max-width: 760px) {
  .shell-navigation { width: 60px; padding-inline: 6px; }
  .shell-navigation > button { justify-content: center; padding-inline: 0; }
  .shell-navigation > button > span:not(:first-child) { display: none; }
  .app-content { left: 60px; }
  .app-content > div { padding: 20px; }
  .collection-layout > .inspector-pane { grid-template-columns: 120px minmax(0, 1fr); gap: 20px; }
  .collection-layout > .inspector-pane h2 { font-size: 24px; }
  .collection-layout > .inspector-pane dl { grid-template-columns: 1fr; }
  .track-detail > div:first-child { flex-direction: column; }
  .track-detail > div:first-child > img { width: 180px; height: 180px; }
  .download-shelf { gap: 8px; padding-inline: 12px; }
}
''')
