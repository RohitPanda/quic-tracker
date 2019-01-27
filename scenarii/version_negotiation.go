package scenarii

import (
	qt "github.com/RohitPanda/quic-tracker"

	"time"
	"github.com/RohitPanda/quic-tracker/agents"
)

const (
	VN_NotAnsweringToVN               = 1
	VN_DidNotEchoVersion              = 2 // draft-07 and below were stating that VN packets should echo the version of the client. It is not used anymore
	VN_LastTwoVersionsAreActuallySeal = 3 // draft-05 and below used AEAD to seal cleartext packets, VN packets should not be sealed, but some implementations did anyway.
	VN_Timeout                        = 4
	VN_UnusedFieldIsIdentical         = 5 // See https://github.com/quicwg/base-drafts/issues/963
)

const ForceVersionNegotiation = 0x1a2a3a4a

type VersionNegotiationScenario struct {
	AbstractScenario
}

func NewVersionNegotiationScenario() *VersionNegotiationScenario {
	return &VersionNegotiationScenario{AbstractScenario{name: "version_negotiation", version: 2}}
}
func (s *VersionNegotiationScenario) Run(conn *qt.Connection, trace *qt.Trace, preferredUrl string, debug bool) {
	s.timeout = time.NewTimer(10 * time.Second)
	connAgents := agents.AttachAgentsToConnection(conn, agents.GetDefaultAgents()...)
	defer connAgents.StopAll()

	incPackets := make(chan interface{}, 1000)
	conn.IncomingPackets.Register(incPackets)

	conn.Version = ForceVersionNegotiation
	trace.ErrorCode = VN_Timeout
	initial := conn.GetInitialPacket()
	conn.SendPacket(initial, qt.EncryptionLevelInitial)

	threshold := 3
	vnCount := 0
	var unusedField byte
	for {
		select {
		case i := <-incPackets:
			switch p := i.(type) {
			case *qt.VersionNegotiationPacket:
				vnCount++
				if vnCount > 1 && unusedField != p.UnusedField {
					trace.ErrorCode = 0
					return
				} else if vnCount == threshold {
					trace.ErrorCode = VN_UnusedFieldIsIdentical
					return
				}
				unusedField = p.UnusedField
				trace.Results["supported_versions"] = p.SupportedVersions // TODO: Compare versions announced ?
				newInitial := qt.NewInitialPacket(conn)
				newInitial.Frames = initial.Frames
				conn.SendPacket(newInitial, qt.EncryptionLevelInitial)
			case qt.Packet:
				trace.MarkError(VN_NotAnsweringToVN, "", p)
				trace.Results["received_packet_type"] = p.Header().PacketType()
			}
			case <-s.Timeout().C:
				return
		}
	}
}
