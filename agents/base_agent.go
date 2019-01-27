//
// This package contains pieces of behaviours that constitutes a QUIC client.
//
// Each agent is responsible for a limited part of the behaviour of a QUIC client. This allows modularity when defining
// test scenarii with specific needs. Each agent is described in its type documentation. For more information on the
// architecture of RohitPanda, please consult the package quictracker documentation.
//
package agents

import (
	. "github.com/RohitPanda/quic-tracker"
	"log"
	"os"
	"fmt"
	"encoding/hex"
	"time"
)

type Agent interface {
	Name() string
	Init(name string, SCID ConnectionID)
	Run(conn *Connection)
	Stop()
	Join()
}

// All agents should embed this structure
type BaseAgent struct {
	name   string
	Logger *log.Logger
	close  chan bool
	closed chan bool
}

func (a *BaseAgent) Name() string { return a.name }

// All agents that embed this structure must call Init() as soon as their Run() method is called
func (a *BaseAgent) Init(name string, ODCID ConnectionID) {
	a.name = name
	a.Logger = log.New(os.Stderr, fmt.Sprintf("[%s/%s] ", hex.EncodeToString(ODCID), a.Name()), log.Lshortfile)
	a.Logger.Println("Agent started")
	a.close = make(chan bool)
	a.closed = make(chan bool)
}

func (a *BaseAgent) Stop() {
	select {
	case <-a.close:
	default:
		close(a.close)
	}
}

func (a *BaseAgent) Join() {
	<-a.closed
}

// Represents a set of agents that are attached to a particular connection
type ConnectionAgents struct {
	conn   *Connection
	agents map[string]Agent
}

func AttachAgentsToConnection(conn *Connection, agents ...Agent) *ConnectionAgents {
	c := ConnectionAgents{conn, make(map[string]Agent)}

	for _, a := range agents {
		c.Add(a)
	}

	return &c
}

func (c *ConnectionAgents) Add(agent Agent) {
	agent.Run(c.conn)
	c.agents[agent.Name()] = agent
}

func (c *ConnectionAgents) Get(name string) Agent {
	return c.agents[name]
}

func (c *ConnectionAgents) StopAll() {
	for _, a := range c.agents {
		a.Stop()
		a.Join()
	}
}

// This function sends an (CONNECTION|APPLICATION)_CLOSE frame and wait for it to be sent out. Then it stops all the
// agents attached to this connection.
func (c *ConnectionAgents) CloseConnection(quicLayer bool, errorCode uint16, reasonPhrase string) {
	a := &ClosingAgent{QuicLayer: quicLayer, ErrorCode: errorCode, ReasonPhrase: reasonPhrase}
	c.Add(a)
	a.Join()
	c.StopAll()
}

// Returns the agents needed for a basic QUIC connection to operate
func GetDefaultAgents() []Agent {
	return []Agent{
		&SocketAgent{},
		&ParsingAgent{},
		&BufferAgent{},
		&TLSAgent{},
		&AckAgent{},
		&SendingAgent{MTU: 1200},
		&RecoveryAgent{TimerValue: 500 * time.Millisecond},
		&RTTAgent{},
	}
}
