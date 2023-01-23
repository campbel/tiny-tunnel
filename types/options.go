package types

type ServerOptions struct {
	Port string `opts:"-p,--port"`
}

type ClientOptions struct {
	Target     string            `opts:"[0]"              desc:"target to proxy"`
	Name       string            `opts:"-n,--name"        desc:"name of the tunnel"`
	ServerHost string            `opts:"-s,--server-host" desc:"server hostname"`
	ServerPort string            `opts:"-p,--server-port" desc:"server port"`
	Insecure   bool              `opts:"-k,--insecure"    desc:"use insecure HTTP and WebSockets"`
	Headers    map[string]string `opts:"-h,--header"      desc:"headers to add to requests"`
}

func (c ClientOptions) SchemeHTTP() string {
	if c.Insecure {
		return "http"
	}
	return "https"
}

func (c ClientOptions) SchemeWS() string {
	if c.Insecure {
		return "ws"
	}
	return "wss"
}
