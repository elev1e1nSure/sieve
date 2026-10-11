//go:build !windows

package maintenance

// TunnelRoute is Windows-only; elsewhere sieve never sifts.
func TunnelRoute() (string, error) {
	return "", nil
}
