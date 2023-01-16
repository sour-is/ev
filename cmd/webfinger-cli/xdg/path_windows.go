//go:build windows
// +build windows

package xdg

func literal(name string) string {
	return "%" + name + "%"
}

const (
	defaultDataHome   = `%LOCALAPPDATA%`
	defaultDataDirs   = `%APPDATA%\Roaming;%ProgramData%`
	defaultConfigHome = `%LOCALAPPDATA%`
	defaultConfigDirs = `%ProgramData%`
	defaultCacheHome  = `%LOCALAPPDATA%\cache`
	defaultStateHome  = `%LOCALAPPDATA%\state`
	defaultRuntime    = `%LOCALAPPDATA%`

	defaultDesktop   = `%USERPROFILE%\Desktop`
	defaultDownload  = `%USERPROFILE%\Downloads`
	defaultDocuments = `%USERPROFILE%\Documents`
	defaultMusic     = `%USERPROFILE%\Music`
	defaultPictures  = `%USERPROFILE%\Pictures`
	defaultVideos    = `%USERPROFILE%\Videos`
	defaultTemplates = `%USERPROFILE%\Templates`
	defaultPublic    = `%USERPROFILE%\Public`

	defaultApplicationDirs = `%APPDATA%\Roaming\Microsoft\Windows\Start Menu\Programs`
	defaultFontDirs        = `%windir%\Fonts;%LOCALAPPDATA%\Microsoft\Windows\Fonts`
)
