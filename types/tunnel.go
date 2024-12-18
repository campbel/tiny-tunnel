package types

type Tunnel struct {
	ID         string
	C          chan (Message)
	AllowedIPs []string
}
