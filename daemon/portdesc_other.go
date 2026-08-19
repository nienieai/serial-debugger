//go:build !windows

package main

func loadPortDescriptions() map[string]string {
	return make(map[string]string)
}
