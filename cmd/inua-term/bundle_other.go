//go:build !darwin

package main

// bundleEnv readies a macOS app bundle's GTK; elsewhere there is none.
func bundleEnv() {}
