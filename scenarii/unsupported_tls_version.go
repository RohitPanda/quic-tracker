package scenarii

import (
	qt "github.com/RohitPanda/quic-tracker"
	"bytes"

	"time"
	"github.com/RohitPanda/quic-tracker/agents"
)

const (
	UTS_NoConnectionCloseSent        = 1
	UTS_WrongErrorCodeIsUsed         = 2 // See https://tools.ietf.org/html/draft-ietf-quic-tls-10#section-11
	UTS_VNDidNotComplete             = 3
	UTS_ReceivedUnexpectedPacketType = 4
)

type UnsupportedTLSVersionScenario struct {
	AbstractScenario
}

func NewUnsupportedTLSVersionScenario() *UnsupportedTLSVersionScenario {
	return &UnsupportedTLSVersionScenario{AbstractScenario{name: "unsupported_tls_version", version: 1}}
}
func (s *UnsupportedTLSVersionScenario) Run(conn *qt.Connection, trace *qt.Trace, preferredUrl string, debug bool) {
	s.timeout = time.NewTimer(10 * time.Second)
	connAgents := agents.AttachAgentsToConnection(conn, agents.GetDefaultAgents()...)
	connAgents.Get("TLSAgent").(*agents.TLSAgent).DisableFrameSending = true
	defer connAgents.StopAll()

	incPackets := make(chan interface{}, 1000)
	conn.IncomingPackets.Register(incPackets)

	sendUnsupportedInitial(conn)

	var connectionClosed bool
forLoop:
	for {
		select {
		case i := <-incPackets:
			switch p := i.(type) {
			case *qt.VersionNegotiationPacket:
				if err := conn.ProcessVersionNegotation(p); err != nil {
					trace.MarkError(UTS_VNDidNotComplete, err.Error(), p)
					return
				}
				sendUnsupportedInitial(conn)
			case *qt.RetryPacket:
				conn.DestinationCID = p.Header().(*qt.LongHeader).SourceCID
				conn.TransitionTo(qt.QuicVersion, qt.QuicALPNToken)
				conn.Token = p.RetryToken
				sendUnsupportedInitial(conn)
			case qt.Framer:
				for _, frame := range p.GetFrames() {
					if cc, ok := frame.(*qt.ConnectionCloseFrame); ok { // See https://tools.ietf.org/html/draft-ietf-quic-tls-10#section-11
						if cc.ErrorCode != 0x146 {  // TLS Alert: procotol_version
							trace.MarkError(UTS_WrongErrorCodeIsUsed, "", p)
						}
						trace.Results["connection_reason_phrase"] = cc.ReasonPhrase
						connectionClosed = true
					}
				}
			case qt.Packet:
				trace.MarkError(UTS_ReceivedUnexpectedPacketType, "", p)
			}
			case <-s.Timeout().C:
				break forLoop
		}
	}

	if !connectionClosed {
		trace.ErrorCode = UTS_NoConnectionCloseSent
	}
}

func sendUnsupportedInitial(conn *qt.Connection) {
	initialPacket := conn.GetInitialPacket()
	for _, f := range initialPacket.Frames { // Advertise support of TLS 1.3 draft-00 only
		if frame, ok := f.(*qt.CryptoFrame); ok {
			frame.CryptoData = bytes.Replace(frame.CryptoData, []byte{0x0, 0x2b, 0x0, 0x03, 0x2, 0x03, 0x04}, []byte{0x0, 0x2b, 0x0, 0x03, 0x2, 0x7f, 0x00}, 1)
		}
	}
	conn.SendPacket(initialPacket, qt.EncryptionLevelInitial)
}
