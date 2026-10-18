package gamestream

import (
	"context"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// Found is a host that announced itself on the local network.
type Found struct {
	Name string // the instance's name: Sunshine's host name
	Addr string // host:port of its HTTP port
}

// service is what GameStream hosts announce (Sunshine through Avahi).
const service = "_nvstream._tcp.local."

var mdnsGroup = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

// Browse asks the local network for GameStream hosts, for wait, and
// returns those that answered. The query is sent from a port of its own,
// so responders answer it directly (RFC 6762, legacy unicast).
func Browse(ctx context.Context, wait time.Duration) ([]Found, error) {
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{})
	b.StartQuestions()
	b.Question(dnsmessage.Question{Name: dnsmessage.MustNewName(service), Type: dnsmessage.TypePTR, Class: dnsmessage.ClassINET})
	query, err := b.Finish()
	if err != nil {
		return nil, err
	}
	if _, err := conn.WriteToUDP(query, mdnsGroup); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetReadDeadline(deadline)
	r := &records{srv: map[string]dnsmessage.SRVResource{}, a: map[string]net.IP{}}
	buf := make([]byte, 9000)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			break
		}
		r.read(buf[:n], from.IP)
	}
	return r.found(), nil
}

// records gathers what the answers said: instances, their services, and
// the addresses of their hosts.
type records struct {
	instances []string
	srv       map[string]dnsmessage.SRVResource
	a         map[string]net.IP
	from      map[string]net.IP
}

func (r *records) read(msg []byte, from net.IP) {
	var p dnsmessage.Parser
	if _, err := p.Start(msg); err != nil {
		return
	}
	p.SkipAllQuestions()
	if r.from == nil {
		r.from = map[string]net.IP{}
	}
	for section := 0; section < 3; section++ {
		for {
			var h dnsmessage.ResourceHeader
			var err error
			switch section {
			case 0:
				h, err = p.AnswerHeader()
			case 1:
				h, err = p.AuthorityHeader()
			default:
				h, err = p.AdditionalHeader()
			}
			if err != nil {
				break
			}
			name := strings.ToLower(h.Name.String())
			switch h.Type {
			case dnsmessage.TypePTR:
				ptr, err := p.PTRResource()
				if err == nil && name == service {
					inst := ptr.PTR.String() // as named, for showing
					r.instances = append(r.instances, inst)
					r.from[strings.ToLower(inst)] = from
				}
			case dnsmessage.TypeSRV:
				if srv, err := p.SRVResource(); err == nil {
					r.srv[name] = srv
				}
			case dnsmessage.TypeA:
				if a, err := p.AResource(); err == nil {
					r.a[name] = net.IP(a.A[:])
				}
			default:
				switch section {
				case 0:
					p.SkipAnswer()
				case 1:
					p.SkipAuthority()
				default:
					p.SkipAdditional()
				}
			}
		}
	}
}

func (r *records) found() []Found {
	var out []Found
	seen := map[string]bool{}
	for _, inst := range r.instances {
		key := strings.ToLower(inst)
		if seen[key] {
			continue
		}
		seen[key] = true
		name := inst[:len(inst)-min(len(inst), len(service)+1)]
		port := DefaultPort
		ip := r.from[key]
		if srv, ok := r.srv[key]; ok {
			port = int(srv.Port)
			if a, ok := r.a[strings.ToLower(srv.Target.String())]; ok {
				ip = a
			}
		}
		if ip == nil {
			continue
		}
		out = append(out, Found{Name: unescapeInstance(name), Addr: net.JoinHostPort(ip.String(), strconv.Itoa(port))})
	}
	return out
}

// unescapeInstance undoes DNS-SD's escapes in an instance's name (\032 a
// space, \. a dot).
func unescapeInstance(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && isDigit(s[i+1]) && isDigit(s[i+2]) && isDigit(s[i+3]) {
			b.WriteByte(byte((int(s[i+1]-'0')*100 + int(s[i+2]-'0')*10 + int(s[i+3]-'0')) & 0xFF))
			i += 3
			continue
		}
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
