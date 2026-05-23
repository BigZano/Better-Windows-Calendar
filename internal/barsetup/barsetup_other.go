//go:build !windows && !linux

package barsetup

// RunSetup is a no-op on unsupported platforms.
func RunSetup(_ string) Result {
	return Result{
		KomorebiBar: StatusNotFound,
		Waybar:      StatusNotFound,
		Polybar:     StatusNotFound,
	}
}
