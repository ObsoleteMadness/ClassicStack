//go:build !darwin

package ltoudp

// triggerLocalNetworkPrivacyAlert is a Darwin-only TCC prompt; other OSes
// have no equivalent Local Network privacy gate on UDP multicast.
func triggerLocalNetworkPrivacyAlert() {}
