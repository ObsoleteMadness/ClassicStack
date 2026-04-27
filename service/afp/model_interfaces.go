//go:build afp

package afp

// RequestModel is implemented by decoded AFP request payload types.
// Callers construct an empty request model and fill it via Unmarshal.
type RequestModel interface {
	String() string
	Unmarshal(data []byte) error
}

// ResponseModel is implemented by encoded AFP response payload types.
// Callers populate a response model and serialize it via Marshal.
type ResponseModel interface {
	String() string
	Marshal() []byte
}
