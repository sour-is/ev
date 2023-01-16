//go:build linux
// +build linux

package xdg

func literal(name string) string {
	return "$" + name
}

const (
	defaultDataHome   = "~/.local/share"
	defaultDataDirs   = "/usr/local/share:/usr/share"
	defaultConfigHome = "~/.config"
	defaultConfigDirs = "/etc/xdg"
	defaultCacheHome  = "~/.local/cache"
	defaultStateHome  = "~/.local/state"
	defaultRuntime    = "/run/user/$UID"

	defaultDesktop   = "~/Desktop"
	defaultDownload  = "~/Downloads"
	defaultDocuments = "~/Documents"
	defaultMusic     = "~/Music"
	defaultPictures  = "~/Pictures"
	defaultVideos    = "~/Videos"
	defaultTemplates = "~/Templates"
	defaultPublic    = "~/Public"

	defaultApplicationDirs = "~/Applications:/Applications"
	defaultFontDirs        = "~/.local/share/fonts:/usr/local/share/fonts:/usr/share/fonts:~/.fonts"
)
