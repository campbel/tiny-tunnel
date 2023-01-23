package types

type ServerOptions struct {
	Port string `opts:"-p,--port" desc:"server port" default:"8000"`
}

type EchoOptions struct {
	Port string `opts:"-p,--port" desc:"server port" default:"7000"`
}

type ClientOptions struct {
	Target     string            `opts:"[0]"              desc:"target to proxy"`
	Name       string            `opts:"-n,--name"        desc:"name of the tunnel"`
	ServerHost string            `opts:"-s,--server-host" desc:"server hostname"                  default:"tt.campbel.io"`
	ServerPort string            `opts:"-p,--server-port" desc:"server port"                      default:"443"`
	Insecure   bool              `opts:"-k,--insecure"    desc:"use insecure HTTP and WebSockets" default:"false"`
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
