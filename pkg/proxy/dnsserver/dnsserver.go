package dnsserver

import (
	"errors"
	"net"
	"strconv"
	"strings"

	"github.com/miekg/dns"
	"github.com/rs/zerolog/log"
)

// dns server
type server struct {
	config *dns.ClientConfig
}

// constants
const resolvFile = "/etc/resolv.conf"

// NewDNSServerDefault create default dns server
func NewDNSServerDefault() (srv *dns.Server) {
	srv = &dns.Server{Addr: ":" + strconv.Itoa(53), Net: "udp"}
	config, _ := dns.ClientConfigFromFile(resolvFile)

	srv.Handler = &server{config}

	log.Info().Msgf("Successful load local " + resolvFile)
	for _, server := range config.Servers {
		log.Info().Msgf("Success load nameserver %s\n", server)
	}
	for _, domain := range config.Search {
		log.Info().Msgf("Success load search %s\n", domain)
	}
	return
}

//ServeDNS query DNS rescord
func (s *server) ServeDNS(w dns.ResponseWriter, req *dns.Msg) {
	msg := dns.Msg{}
	msg.SetReply(req)
	msg.Authoritative = true
	// Stuff must be in the answer section
	for _, a := range s.query(req) {
		log.Info().Msgf("%v\n", a)
		msg.Answer = append(msg.Answer, a)
	}

	_ = w.WriteMsg(&msg)
}

func (s *server) getDomain(origin string, stripPostfix bool) string {
	domain := origin
	postfix := s.config.Search[0]

	// should not happens, just in case
	if !strings.Contains(domain, ".") {
		domain = domain + "."
	}

	// strip domain postfix, if required
	dotIndex := strings.Index(domain, ".") + 1
	if stripPostfix {
		domain = domain[:dotIndex]
	}

	// has only one dot at the end of queried domain name
	if dotIndex == len(domain) {
		domain = domain + postfix + "."
		log.Info().Msgf("Format domain %s to %s\n", origin, domain)
	}

	return domain
}

func (s *server) query(req *dns.Msg) (rr []dns.RR) {
	if len(req.Question) <= 0 {
		log.Error().Msgf("*** error: dns Msg question length is 0")
		return
	}

	qtype := req.Question[0].Qtype
	name := req.Question[0].Name

	rr, err := s.exchange(s.getDomain(name, false), qtype, name)
	if IsDomainNotExist(err) {
		log.Info().Msgf("Retry with domain postfix stripped")
		rr, _ = s.exchange(s.getDomain(name, true), qtype, name)
	}
	return
}

func (s *server) getResolvServer() (address string, err error) {
	if len(s.config.Servers) <= 0 {
		err = errors.New("*** error: dns server is 0")
		return
	}

	server := s.config.Servers[0]
	port := s.config.Port

	address = net.JoinHostPort(server, port)
	return
}

func (s *server) exchange(domain string, qtype uint16, name string) (rr []dns.RR, err error) {
	log.Info().Msgf("Received DNS query for %s: \n", domain)
	address, err := s.getResolvServer()
	if err != nil {
		log.Error().Msgf(err.Error())
		return
	}
	log.Info().Msgf("Exchange message for domain %s to dns server %s\n", domain, address)

	c := new(dns.Client)
	msg := new(dns.Msg)
	msg.RecursionDesired = true
	msg.SetQuestion(domain, qtype)
	res, _, err := c.Exchange(msg, address)

	if res == nil {
		if err != nil {
			log.Error().Msgf("*** error: %s\n", err.Error())
		} else {
			log.Error().Msgf("*** error: unknown\n")
		}
		return
	}

	if res.Rcode == dns.RcodeNameError {
		err = DomainNotExistError{domain}
		return
	} else if res.Rcode != dns.RcodeSuccess {
		log.Error().Msgf(" *** failed to answer name %s after %d query for %s\n", name, qtype, domain)
		return
	}

	for _, item := range res.Answer {
		log.Info().Msgf("response: %s", item.String())
		r, errInLoop := s.getAnswer(name, domain, item)
		if errInLoop != nil {
			err = errInLoop
			return
		}
		rr = append(rr, r)
	}

	return
}

func (s *server) getAnswer(name string, inClusterName string, acutal dns.RR) (tmp dns.RR, err error) {
	if name != inClusterName {
		log.Info().Msgf("origin %s query name is not same %s", inClusterName, name)
		log.Info().Msgf("origin answer rr to %s", acutal.String())

		var parts []string
		parts = append(parts, name)
		answer := strings.Split(acutal.String(), "\t")
		parts = append(parts, answer[1:]...)

		rrStr := strings.Join(parts, " ")
		log.Info().Msgf("rewrite rr to %s", rrStr)
		tmp, err = dns.NewRR(rrStr)
		if err != nil {
			return
		}
	} else {
		tmp = acutal
	}
	return
}
