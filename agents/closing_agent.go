package agents

import . "github.com/RohitPanda/quic-tracker"

// The ClosingAgent is responsible for queuing an (CONNECTION|APPLICATION)_CLOSE frame and to wait for it to be sent out.
type ClosingAgent struct {
	BaseAgent
	QuicLayer bool
	ErrorCode uint16
	ReasonPhrase string
}

func (a *ClosingAgent) Run (conn *Connection) {
	a.Init("ClosingAgent", conn.OriginalDestinationCID)

	outgoingPackets := make(chan interface{}, 1000)
	conn.OutgoingPackets.Register(outgoingPackets)

	go func() {
		defer a.Logger.Println("Agent terminated")
		defer close(a.closed)

		conn.CloseConnection(a.QuicLayer, a.ErrorCode, a.ReasonPhrase)
		for {
			switch p := (<-outgoingPackets).(type) {
			case Framer:
				if p.Contains(ConnectionCloseType) || p.Contains(ApplicationCloseType) {
					return
				}
			}
		}
	}()
}
