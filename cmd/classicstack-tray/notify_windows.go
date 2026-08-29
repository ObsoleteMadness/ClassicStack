//go:build windows

package main

import (
	"fmt"
	"os"

	"github.com/go-toast/toast"
)

// showNotification raises a Windows Action Center toast. Unlike macOS,
// clicking it genuinely activates activateURL: toast.Notification defaults
// ActivationType to "protocol", so setting ActivationArguments to the
// control API's base URL hands it straight to the OS's URL handler (the
// default browser) on click — real click-to-open, not just a visible
// banner (contrast notify_darwin.go's documented limitation there).
func showNotification(title, message, activateURL string) {
	n := toast.Notification{
		AppID:               "ClassicStack",
		Title:               title,
		Message:             message,
		ActivationArguments: activateURL,
	}
	if err := n.Push(); err != nil {
		fmt.Fprintf(os.Stderr, "classicstack-tray: notification failed: %v\n", err)
	}
}
