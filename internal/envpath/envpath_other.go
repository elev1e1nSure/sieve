//go:build !windows

package envpath

type Result struct {
	Dir            string
	Added          bool
	AlreadyPresent bool
	Skipped        bool
	Reason         string
}

func EnsureExecutableDir() (Result, error) {
	return Result{
		Skipped: true,
		Reason:  "PATH auto-add is only supported on Windows",
	}, nil
}
