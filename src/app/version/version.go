package version

const (
	ProjectName     = "fastoshop"
	ShareFolderPath = "/usr/share/fastoshop"
	ConfigPath      = "/etc/fastoshop.conf"
)

// VersionApp подставляется линковщиком из тега сборки (см. LDFLAGS в Makefile).
// Значение по умолчанию видно только при сборке из исходников вручную.
var VersionApp = "dev"
