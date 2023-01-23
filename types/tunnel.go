package types

type Tunnel struct {
	ID         string
	C          chan (Request)
	AllowedIPs []string
}
