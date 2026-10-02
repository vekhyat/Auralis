package ipod

// volumeInfo is one mounted filesystem Auralis can inspect.
type volumeInfo struct {
	Root       string
	Label      string
	FileSystem string
	Total      uint64
	Free       uint64
}
