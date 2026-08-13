package version

const (
	ProjectName     = "fastoshop"
	ShareFolderPath = "/usr/share/fastoshop"
	ConfigPath      = "/etc/fastoshop.conf"
)

// VersionApp is injected by the linker from the build tag (see LDFLAGS in the
// Makefile). The default value is only visible when building from source by hand.
var VersionApp = "dev"
