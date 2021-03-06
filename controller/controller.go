package controller

import (
	"./channel/httpCovert"
	"./channel/httpNormal"
	"./channel/icmpIP"
	"./channel/icmpNormal"
	"./channel/tcpHandshake"
	"./channel/tcpNormal"
	"./channel/tcpSyn"
	"./channel/udpIP"
	"./channel/udpNormal"
	"./config"
	"./processor/asymmetricEncryption"
	"./processor/caesar"
	"./processor/checksum"
	"./processor/gZipCompression"
	"./processor/none"
	"./processor/symmetricEncryption"
	"./processor/zLibCompression"
	"encoding/json"
	"errors"
	"github.com/gorilla/websocket"
	"log"
	"os"
	"strconv"
	"time"
)

// Constructor for the controller
func CreateController() (*Controller, error) {
	var ctr *Controller = &Controller{
		config:     DefaultConfig(),
		clients:    make(map[*websocket.Conn]bool),
		clientStop: make(chan interface{}),
		recvStop:   make(chan interface{}),
		sendStop:   make(chan interface{}),
		doneWsSend: make(chan interface{}),
		doneWsRecv: make(chan interface{}),
		wsSend:     make(chan []byte),
		wsRecv:     make(chan []byte),
	}
	// Validate the default values
	if err := config.ValidateConfigSet(ctr.config.Default.Processor); err != nil {
		return nil, err
	}
	if err := config.ValidateConfigSet(ctr.config.Default.Channel); err != nil {
		return nil, err
	}
	// Validate all of the active processor configs
	for i := range ctr.config.Processors {
		if err := config.ValidateConfigSet(ctr.config.Processors[i].Data); err != nil {
			return nil, err
		}
	}
	// Validate the active channel config
	if err := config.ValidateConfigSet(ctr.config.Channel.Data); err != nil {
		return nil, err
	}
	go ctr.webReceiveLoop()
	go ctr.webSendLoop()
	return ctr, nil
}

// A default config for the system and all Covert Channels
func DefaultConfig() configData {
	return configData{
		OpCode: "config",
		Default: defaultConfig{
			Processor: defaultProcessor(),
			Channel:   defaultChannel(),
		},
		Processors: []processorConfig{},
		Channel: channelConfig{
			Type: "TcpHandshake",
			Data: defaultChannel(),
		},
	}
}

func defaultChannel() channelData {
	return channelData{
		IcmpIP:       icmpIP.GetDefault(),
		IcmpNormal:   icmpNormal.GetDefault(),
		TcpSyn:       tcpSyn.GetDefault(),
		TcpHandshake: tcpHandshake.GetDefault(),
		TcpNormal:    tcpNormal.GetDefault(),
		HttpCovert:   httpCovert.GetDefault(),
		HttpNormal:   httpNormal.GetDefault(),
		UdpNormal:    udpNormal.GetDefault(),
		UdpIP:        udpIP.GetDefault(),
	}
}

func defaultProcessor() processorData {
	return processorData{
		None:                 none.GetDefault(),
		Caesar:               caesar.GetDefault(),
		Checksum:             checksum.GetDefault(),
		SymmetricEncryption:  symmetricEncryption.GetDefault(),
		AsymmetricEncryption: asymmetricEncryption.GetDefault(),
		GZipCompression:      gZipCompression.GetDefault(),
		ZLibCompression:      zLibCompression.GetDefault(),
	}
}

// Callback when receiving a message from the client
func (ctr *Controller) handleMessage(data []byte) []byte {
	var cmd command
	if err := json.Unmarshal(data, &cmd); err != nil {
		return toMessage("error", "Unable to read command: "+err.Error())
	}

	// Determine the operation to perform
	switch cmd.OpCode {
	case "open":
		// Close a channel if it is already open
		if err := ctr.handleClose(); err != nil {
			return toMessage("error", "Unable to close channel: "+err.Error())
		} else if err := ctr.handleOpen(data); err != nil {
			return toMessage("error", "Unable to open channel: "+err.Error())
		} else {
			go ctr.readLoop()
			return toMessage("open", "Open success")
		}
	case "close":
		if err := ctr.handleClose(); err != nil {
			return toMessage("error", "Unable to close channel: "+err.Error())
		} else {
			return toMessage("close", "Close success")
		}
	case "write":
		if err := ctr.handleWrite(data); err != nil {
			return toMessage("error", "Unable to write to channel: "+err.Error())
		} else {
			return toMessage("write", "Message write success")
		}
	case "config":
		if data, err := ctr.handleConfig(); err != nil {
			return toMessage("error", "Could not encode config: "+err.Error())
		} else {
			return data
		}
	default:
		return toMessage("error", "Unknown operation code")
	}
}

// A helper function for preparing responses to the client
// opcode is the type of message, and is one of the valid opCodes from the client or "error"
// data is the message
func toMessage(opcode string, data string) []byte {
	var mt messageType
	mt.OpCode = opcode
	mt.Message = data
	if data, err := json.Marshal(mt); err != nil {
		return []byte("{\"OpCode\" : \"error\", \"Message\" : \"Marshal Error\" }")
	} else {
		return data
	}
}

// Handle the config command
func (ctr *Controller) handleConfig() ([]byte, error) {
	ctr.config.OpCode = "config"
	if data, err := json.Marshal(ctr.config); err != nil {
		return nil, err
	} else {
		return data, nil
	}
}

// Handle the write command
func (ctr *Controller) handleWrite(b []byte) error {
	var (
		mt   messageType
		err  error
		data []byte
	)
	if err = json.Unmarshal(b, &mt); err != nil {
		return err
	}
	if ctr.layers == nil {
		return errors.New("Channel closed")
	}

	data = []byte(mt.Message)
	for i := range ctr.layers.processors {
		if data, err = ctr.layers.processors[i].Process(data); err != nil {
			return errors.New("Unable to process outgoing message: " + err.Error())
		}
	}
	if n, err := ctr.layers.channel.Send(data); err != nil {
		return errors.New("Write fail: Wrote " + strconv.FormatUint(n, 10) + "bytes out of " + strconv.FormatUint(uint64(len(b)), 10) + ": " + err.Error())
	} else {
		return nil
	}
}

// Handle a read operation
func (ctr *Controller) handleRead() ([]byte, error) {

	var (
		buffer [1024]byte
		data   []byte
	)

	if n, err := ctr.layers.channel.Receive(buffer[:]); err != nil {
		return nil, errors.New("Read fail: Read " + strconv.FormatUint(n, 10) + " bytes out of " + strconv.FormatUint(uint64(len(buffer)), 10) + " available bytes: " + err.Error())
	} else {
		data = buffer[:n]
		for i := len(ctr.layers.processors) - 1; i >= 0; i-- {
			if data, err = ctr.layers.processors[i].Unprocess(data); err != nil {
				return nil, errors.New("Unable to unprocess incoming message: " + err.Error())
			}
		}
	}
	return data, nil
}

// Loop for repeatedly reading from  any open Covert Channel
func (ctr *Controller) readLoop() {
loop:
	for {
		select {
		case <-ctr.layers.readClose:
			close(ctr.layers.readCloseDone)
			break loop
		default:
			data, err := ctr.handleRead()
			if err != nil {
				// First, check if we are closing the covert channel
				// If so, then that is the likely explanation of the error
				// and we don't neet to report it
				select {
				case <-ctr.layers.readClose:
				default:
					// Else we try to report the error
					// This select also includes the readClose
					// to handle the case where the server is being shutdown
					select {
					case ctr.wsSend <- toMessage("error", err.Error()):
						// If there has been a read error wait
						// to avoid a constant stream of data
						// to the UI
						time.Sleep(time.Second)
						// If we have closed we return immediately
					case <-ctr.layers.readClose:
					}
				}
			} else {
				ctr.wsSend <- toMessage("read", string(data))
			}
		}
	}
}

// Handle the close operation
func (ctr *Controller) handleClose() error {
	var err error
	if ctr.layers != nil {

		close(ctr.layers.readClose)
		err = ctr.layers.channel.Close()

		// We must wait to ensure that the read loop is complete
		// In case closing the channel failed to cause handleRead to return
		select {
		case <-ctr.layers.readCloseDone:
		case <-time.After(time.Second * 5):
			var l *log.Logger = log.New(os.Stderr, "", log.Flags())
			l.Println("Failed to close read loop. Covert channel did not return from cancel.")
		}
		ctr.layers = nil
	}
	return err
}

// Shutdown the controller
func (ctr *Controller) Shutdown() error {
	return ctr.webShutdown()
}
