package controller

import (
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"math/rand"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestShutdown(t *testing.T) {
	ctr, err := CreateController()
	if err != nil {
		t.Errorf("Unexpected open error: %s", err.Error())
	}
	err = ctr.Shutdown()
	if err != nil {
		t.Errorf("Unexpected close error: %s", err.Error())
	}
}

func openConn(addr string, port string, ctr *Controller, t *testing.T) (chan []byte, chan []byte, chan interface{}, chan interface{}) {

	var (
		write chan []byte      = make(chan []byte, 32)
		read  chan []byte      = make(chan []byte, 32)
		stop  chan interface{} = make(chan interface{})
		done  chan interface{} = make(chan interface{})
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/covert", ctr.HandleFunc)

	srv := &http.Server{Addr: ":" + port, Handler: mux}

	go func() {
		srv.ListenAndServe()
		close(done)
	}()
	// Give time for the server to start
	time.Sleep(time.Millisecond * 100)
	client, _, err := websocket.DefaultDialer.Dial(addr, nil)
	if err != nil {
		t.Errorf("Unexpected dial error: %s", err.Error())
		return write, read, stop, done
	}

	go func() {
	loop:
		for {
			select {
			case data := <-write:
				client.WriteMessage(websocket.TextMessage, data)
			case <-stop:
				break loop
			}
		}
		if err = client.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); err != nil {
			t.Errorf("Unexpected client close error: %s", err.Error())
		}

		if err = client.Close(); err != nil {
			t.Errorf("Unexpected client close error: %s", err.Error())
		}

		if err = ctr.Shutdown(); err != nil {
			t.Errorf("Unexpected controller close error: %s", err.Error())
		}
		ctx, cancelFn := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelFn()
		srv.Shutdown(ctx)
	}()

	go func() {
	loop:
		for {
			select {
			case <-stop:
				break loop
			default:
			}
			_, data, err := client.ReadMessage()
			if err != nil {
				break
			}
			read <- data
		}
	}()

	return write, read, stop, done
}

func TestRetrieveConfig(t *testing.T) {
	ctr, _ := CreateController()

	write, read, stop, done := openConn("ws://127.0.0.1:9020/covert", "9020", ctr, t)

	write <- []byte("{\"OpCode\" : \"config\"}")

	checkConfig(read, DefaultConfig(), t)

	checkClose(stop, done, t)
}

// To confirm that the processing is really occurring,
// we ommit the processors for the receiver side and confirm
// that it changes the output message
func TestWithProcessorNoUnprocess(t *testing.T) {
	ctr1, _ := CreateController()
	ctr2, _ := CreateController()

	write1, read1, stop1, done1 := openConn("ws://127.0.0.1:9030/covert", "9030", ctr1, t)
	write2, read2, stop2, done2 := openConn("ws://127.0.0.1:9040/covert", "9040", ctr2, t)

	write1 <- []byte("{\"OpCode\" : \"config\"}")

	conf := checkConfig(read1, DefaultConfig(), t)

	conf.OpCode = "open"
	conf.Channel.Data.TcpHandshake.FriendReceivePort.Value = 8090
	conf.Channel.Data.TcpHandshake.OriginReceivePort.Value = 8091
	conf.Processors = []processorConfig{
		processorConfig{
			Type: "Caesar", Data: defaultProcessor(),
		},
		processorConfig{
			Type: "Caesar", Data: defaultProcessor(),
		},
	}
	conf.Processors[0].Data.Caesar.Shift.Value = -1
	conf.Processors[1].Data.Caesar.Shift.Value = 3
	writeTestMsg(write1, conf, t)

	conf.Channel.Data.TcpHandshake.FriendReceivePort.Value = 8091
	conf.Channel.Data.TcpHandshake.OriginReceivePort.Value = 8090
	conf.Processors = []processorConfig{}
	writeTestMsg(write2, conf, t)

	checkMsgType(read1, "open", "Open success", t)
	checkMsgType(read2, "open", "Open success", t)

	write1 <- []byte("{\"OpCode\" : \"write\", \"Message\" : \"Hello World!\"}")
	checkMsgType(read2, "read", "Jgnnq\"Yqtnf#", t)

	checkClose(stop1, done1, t)
	checkClose(stop2, done2, t)
}

func checkClose(stop chan interface{}, done chan interface{}, t *testing.T) {
	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second * 5):
		t.Errorf("Unexpected shutdown timeout")
	}
}

func writeTestMsg(ch chan []byte, d interface{}, t *testing.T) {
	if data, err := json.Marshal(d); err != nil {
		t.Errorf("Unexpected marshal error: %s", err.Error())
	} else {
		ch <- data
	}
}

func checkMsgType(ch chan []byte, opcode string, msg string, t *testing.T) {
	select {
	case data := <-ch:
		var mt messageType
		if err := json.Unmarshal(data, &mt); err != nil {
			t.Errorf("Unexpected unmarshal error: %s", err.Error())
		} else {
			if mt.OpCode != opcode {
				t.Errorf("Message does not have correct opcode: %s, want %s", mt.OpCode, opcode)
			}
			if mt.Message != msg {
				t.Errorf("Message does not have correct message: %s, want %s", mt.Message, msg)
			}
		}
	case <-time.After(time.Second * 10):
		t.Errorf("Unexpected read timeout")
	}
}

// Checks that two strings are equal in terms of utf characters
func utf8Equal() {

}

func checkConfig(ch chan []byte, expt configData, t *testing.T) configData {
	var conf configData
	select {
	case data := <-ch:
		if err := json.Unmarshal(data, &conf); err != nil {
			t.Errorf("Unexpected unmarshal error: %s", err.Error())
		} else {
			if !reflect.DeepEqual(conf, expt) {
				t.Errorf("Configs do not match error: \n%v, \n%v", conf, expt)
			}
		}
	case <-time.After(time.Second * 5):
		t.Errorf("Unexpected read timeout")
	}
	return conf
}

type channelTest struct {
	name     string
	f1       func(*configData)
	f2       func(*configData)
	isTiming bool
}

func runMultiChannelWrite(t *testing.T, cl []channelTest, messages []string, timingMessages []string) {
	ctr1, _ := CreateController()
	ctr2, _ := CreateController()

	write1, read1, stop1, done1 := openConn("ws://127.0.0.1:9030/covert", "9030", ctr1, t)
	write2, read2, stop2, done2 := openConn("ws://127.0.0.1:9040/covert", "9040", ctr2, t)

	varCurrConf1 := DefaultConfig()

	for i := range cl {
		var useMessages []string
		if cl[i].isTiming {
			useMessages = timingMessages
		} else {
			useMessages = messages
		}

		write1 <- []byte("{\"OpCode\" : \"config\"}")
		conf := checkConfig(read1, varCurrConf1, t)

		conf.Channel.Type = cl[i].name
		cl[i].f1(&conf)
		varCurrConf1 = conf

		conf.OpCode = "open"
		writeTestMsg(write1, conf, t)
		cl[i].f2(&conf)
		writeTestMsg(write2, conf, t)

		checkMsgType(read1, "open", "Open success", t)
		checkMsgType(read2, "open", "Open success", t)

		for i := range useMessages {
			msg := messageType{OpCode: "write", Message: useMessages[i]}
			if b, err := json.Marshal(msg); err == nil {
				// The marshaller will convert the string into a format that can be interpreted on the other side
				// This will change invalid utf8 into valid, so we must use that string.
				if err := json.Unmarshal(b, &msg); err != nil {
					t.Errorf("Marshal Error: %s", err.Error())
				}

				// Check message sending in both directions
				write1 <- b
				checkMsgType(read1, "write", "Message write success", t)
				checkMsgType(read2, "read", msg.Message, t)
				write2 <- b
				checkMsgType(read2, "write", "Message write success", t)
				checkMsgType(read1, "read", msg.Message, t)
			} else {
				t.Errorf("Marshal Error: %s", err.Error())
			}
		}

		write1 <- []byte("{\"OpCode\" : \"close\"}")
		write2 <- []byte("{\"OpCode\" : \"close\"}")

		checkMsgType(read1, "close", "Close success", t)
		checkMsgType(read2, "close", "Close success", t)
	}

	checkClose(stop1, done1, t)
	checkClose(stop2, done2, t)
}

func TestMessageExchange(t *testing.T) {
	cl := []channelTest{
		channelTest{
			name: "TcpNormal",
			f1: func(conf *configData) {
				conf.Channel.Data.TcpNormal.FriendReceivePort.Value = 8090
				conf.Channel.Data.TcpNormal.OriginReceivePort.Value = 8091
			},
			f2: func(conf *configData) {
				conf.Channel.Data.TcpNormal.FriendReceivePort.Value = 8091
				conf.Channel.Data.TcpNormal.OriginReceivePort.Value = 8090
			},
		},
		channelTest{
			name: "TcpHandshake",
			f1: func(conf *configData) {
				conf.Channel.Data.TcpHandshake.FriendReceivePort.Value = 8090
				conf.Channel.Data.TcpHandshake.OriginReceivePort.Value = 8091
			},
			f2: func(conf *configData) {
				conf.Channel.Data.TcpHandshake.FriendReceivePort.Value = 8091
				conf.Channel.Data.TcpHandshake.OriginReceivePort.Value = 8090
			},
		},
		channelTest{
			name: "TcpHandshake",
			f1: func(conf *configData) {
				conf.Channel.Data.TcpHandshake.FriendReceivePort.Value = 8090
				conf.Channel.Data.TcpHandshake.OriginReceivePort.Value = 8091
				conf.Channel.Data.TcpHandshake.Embedder.Value = "urgflg"
			},
			f2: func(conf *configData) {
				conf.Channel.Data.TcpHandshake.FriendReceivePort.Value = 8091
				conf.Channel.Data.TcpHandshake.OriginReceivePort.Value = 8090
				conf.Channel.Data.TcpHandshake.Embedder.Value = "urgflg"
			},
		},
		channelTest{
			name: "TcpHandshake",
			f1: func(conf *configData) {
				conf.Channel.Data.TcpHandshake.FriendReceivePort.Value = 8090
				conf.Channel.Data.TcpHandshake.OriginReceivePort.Value = 8091
				conf.Channel.Data.TcpHandshake.Embedder.Value = "urgptr"
			},
			f2: func(conf *configData) {
				conf.Channel.Data.TcpHandshake.FriendReceivePort.Value = 8091
				conf.Channel.Data.TcpHandshake.OriginReceivePort.Value = 8090
				conf.Channel.Data.TcpHandshake.Embedder.Value = "urgptr"
			},
		},
		channelTest{
			name: "TcpHandshake",
			f1: func(conf *configData) {
				conf.Channel.Data.TcpHandshake.FriendReceivePort.Value = 9090
				conf.Channel.Data.TcpHandshake.OriginReceivePort.Value = 9091
				conf.Channel.Data.TcpHandshake.Embedder.Value = "ecn"
			},
			f2: func(conf *configData) {
				conf.Channel.Data.TcpHandshake.FriendReceivePort.Value = 9091
				conf.Channel.Data.TcpHandshake.OriginReceivePort.Value = 9090
				conf.Channel.Data.TcpHandshake.Embedder.Value = "ecn"
			},
		},
		channelTest{
			name: "TcpHandshake",
			f1: func(conf *configData) {
				conf.Channel.Data.TcpHandshake.FriendReceivePort.Value = 9090
				conf.Channel.Data.TcpHandshake.OriginReceivePort.Value = 9091
				conf.Channel.Data.TcpHandshake.Embedder.Value = "timestamp"
			},
			f2: func(conf *configData) {
				conf.Channel.Data.TcpHandshake.FriendReceivePort.Value = 9091
				conf.Channel.Data.TcpHandshake.OriginReceivePort.Value = 9090
				conf.Channel.Data.TcpHandshake.Embedder.Value = "timestamp"
			},
			isTiming: true,
		},
		channelTest{
			name: "TcpHandshake",
			f1: func(conf *configData) {
				conf.Channel.Data.TcpHandshake.FriendReceivePort.Value = 9090
				conf.Channel.Data.TcpHandshake.OriginReceivePort.Value = 9091
				conf.Channel.Data.TcpHandshake.Embedder.Value = "temporal"
			},
			f2: func(conf *configData) {
				conf.Channel.Data.TcpHandshake.FriendReceivePort.Value = 9091
				conf.Channel.Data.TcpHandshake.OriginReceivePort.Value = 9090
				conf.Channel.Data.TcpHandshake.Embedder.Value = "temporal"
			},
			isTiming: true,
		},
		channelTest{
			name: "TcpHandshake",
			f1: func(conf *configData) {
				conf.Channel.Data.TcpHandshake.FriendReceivePort.Value = 9090
				conf.Channel.Data.TcpHandshake.OriginReceivePort.Value = 9091
				conf.Channel.Data.TcpHandshake.Embedder.Value = "frequency"
			},
			f2: func(conf *configData) {
				conf.Channel.Data.TcpHandshake.FriendReceivePort.Value = 9091
				conf.Channel.Data.TcpHandshake.OriginReceivePort.Value = 9090
				conf.Channel.Data.TcpHandshake.Embedder.Value = "frequency"
			},
			isTiming: true,
		},
		channelTest{
			name: "TcpHandshake",
			f1: func(conf *configData) {
				conf.Channel.Data.TcpHandshake.FriendReceivePort.Value = 9090
				conf.Channel.Data.TcpHandshake.OriginReceivePort.Value = 9091
				conf.Channel.Data.TcpHandshake.Embedder.Value = "ecntemporal"
			},
			f2: func(conf *configData) {
				conf.Channel.Data.TcpHandshake.FriendReceivePort.Value = 9091
				conf.Channel.Data.TcpHandshake.OriginReceivePort.Value = 9090
				conf.Channel.Data.TcpHandshake.Embedder.Value = "ecntemporal"
			},
			isTiming: true,
		},
		channelTest{
			name: "TcpHandshake",
			f1: func(conf *configData) {
				conf.Channel.Data.TcpHandshake.FriendReceivePort.Value = 8090
				conf.Channel.Data.TcpHandshake.OriginReceivePort.Value = 8091
				conf.Channel.Data.TcpHandshake.Embedder.Value = "id"

				conf.Processors = []processorConfig{
					processorConfig{
						Type: "Caesar", Data: defaultProcessor(),
					},
					processorConfig{
						Type: "Caesar", Data: defaultProcessor(),
					},
				}
			},
			f2: func(conf *configData) {
				conf.Channel.Data.TcpHandshake.FriendReceivePort.Value = 8091
				conf.Channel.Data.TcpHandshake.OriginReceivePort.Value = 8090
				conf.Channel.Data.TcpHandshake.Embedder.Value = "id"

				conf.Processors = []processorConfig{
					processorConfig{
						Type: "Caesar", Data: defaultProcessor(),
					},
					processorConfig{
						Type: "Caesar", Data: defaultProcessor(),
					},
				}
			},
		},
		channelTest{
			name: "TcpHandshake",
			f1: func(conf *configData) {
				conf.Channel.Data.TcpHandshake.FriendReceivePort.Value = 8090
				conf.Channel.Data.TcpHandshake.OriginReceivePort.Value = 8091

				conf.Processors = []processorConfig{
					processorConfig{
						Type: "Caesar", Data: defaultProcessor(),
					},
				}
			},
			f2: func(conf *configData) {
				conf.Channel.Data.TcpHandshake.FriendReceivePort.Value = 8091
				conf.Channel.Data.TcpHandshake.OriginReceivePort.Value = 8090

				conf.Processors = []processorConfig{
					processorConfig{
						Type: "Caesar", Data: defaultProcessor(),
					},
				}
			},
		},
		channelTest{
			name: "TcpSyn",
			f1: func(conf *configData) {
				conf.Channel.Data.TcpSyn.FriendPort.Value = 8090
				conf.Channel.Data.TcpSyn.OriginPort.Value = 8091
				conf.Channel.Data.TcpSyn.Embedder.Value = "sequence"
			},
			f2: func(conf *configData) {
				conf.Channel.Data.TcpSyn.FriendPort.Value = 8091
				conf.Channel.Data.TcpSyn.OriginPort.Value = 8090
				conf.Channel.Data.TcpSyn.Embedder.Value = "sequence"
			},
		},
		channelTest{
			name: "TcpSyn",
			f1: func(conf *configData) {
				conf.Channel.Data.TcpSyn.FriendPort.Value = 8090
				conf.Channel.Data.TcpSyn.OriginPort.Value = 8091
				conf.Channel.Data.TcpSyn.Embedder.Value = "urgflg"
			},
			f2: func(conf *configData) {
				conf.Channel.Data.TcpSyn.FriendPort.Value = 8091
				conf.Channel.Data.TcpSyn.OriginPort.Value = 8090
				conf.Channel.Data.TcpSyn.Embedder.Value = "urgflg"
			},
		},
		channelTest{
			name: "TcpSyn",
			f1: func(conf *configData) {
				conf.Channel.Data.TcpSyn.FriendPort.Value = 8090
				conf.Channel.Data.TcpSyn.OriginPort.Value = 8091
				conf.Channel.Data.TcpSyn.Embedder.Value = "urgptr"
			},
			f2: func(conf *configData) {
				conf.Channel.Data.TcpSyn.FriendPort.Value = 8091
				conf.Channel.Data.TcpSyn.OriginPort.Value = 8090
				conf.Channel.Data.TcpSyn.Embedder.Value = "urgptr"
			},
		},
		channelTest{
			name: "TcpSyn",
			f1: func(conf *configData) {
				conf.Channel.Data.TcpSyn.FriendPort.Value = 8090
				conf.Channel.Data.TcpSyn.OriginPort.Value = 8091
				conf.Channel.Data.TcpSyn.Embedder.Value = "timestamp"
			},
			f2: func(conf *configData) {
				conf.Channel.Data.TcpSyn.FriendPort.Value = 8091
				conf.Channel.Data.TcpSyn.OriginPort.Value = 8090
				conf.Channel.Data.TcpSyn.Embedder.Value = "timestamp"
			},
			isTiming: true,
		},
		channelTest{
			name: "TcpSyn",
			f1: func(conf *configData) {
				conf.Channel.Data.TcpSyn.FriendPort.Value = 8090
				conf.Channel.Data.TcpSyn.OriginPort.Value = 8091
				conf.Channel.Data.TcpSyn.Embedder.Value = "id"
			},
			f2: func(conf *configData) {
				conf.Channel.Data.TcpSyn.FriendPort.Value = 8091
				conf.Channel.Data.TcpSyn.OriginPort.Value = 8090
				conf.Channel.Data.TcpSyn.Embedder.Value = "id"
			},
		},
		channelTest{
			name: "TcpSyn",
			f1: func(conf *configData) {
				conf.Channel.Data.TcpSyn.FriendPort.Value = 8090
				conf.Channel.Data.TcpSyn.OriginPort.Value = 8091
				conf.Channel.Data.TcpSyn.Embedder.Value = "ecn"
			},
			f2: func(conf *configData) {
				conf.Channel.Data.TcpSyn.FriendPort.Value = 8091
				conf.Channel.Data.TcpSyn.OriginPort.Value = 8090
				conf.Channel.Data.TcpSyn.Embedder.Value = "ecn"
			},
		},
		channelTest{
			name: "TcpSyn",
			f1: func(conf *configData) {
				conf.Channel.Data.TcpSyn.FriendPort.Value = 8090
				conf.Channel.Data.TcpSyn.OriginPort.Value = 8091
				conf.Channel.Data.TcpSyn.Embedder.Value = "temporal"
			},
			f2: func(conf *configData) {
				conf.Channel.Data.TcpSyn.FriendPort.Value = 8091
				conf.Channel.Data.TcpSyn.OriginPort.Value = 8090
				conf.Channel.Data.TcpSyn.Embedder.Value = "temporal"
			},
			isTiming: true,
		},
		channelTest{
			name: "TcpSyn",
			f1: func(conf *configData) {
				conf.Channel.Data.TcpSyn.FriendPort.Value = 8090
				conf.Channel.Data.TcpSyn.OriginPort.Value = 8091
				conf.Channel.Data.TcpSyn.Embedder.Value = "frequency"
			},
			f2: func(conf *configData) {
				conf.Channel.Data.TcpSyn.FriendPort.Value = 8091
				conf.Channel.Data.TcpSyn.OriginPort.Value = 8090
				conf.Channel.Data.TcpSyn.Embedder.Value = "frequency"
			},
			isTiming: true,
		},
		channelTest{
			name: "TcpSyn",
			f1: func(conf *configData) {
				conf.Channel.Data.TcpSyn.FriendPort.Value = 8090
				conf.Channel.Data.TcpSyn.OriginPort.Value = 8091
				conf.Channel.Data.TcpSyn.Embedder.Value = "ecntemporal"
			},
			f2: func(conf *configData) {
				conf.Channel.Data.TcpSyn.FriendPort.Value = 8091
				conf.Channel.Data.TcpSyn.OriginPort.Value = 8090
				conf.Channel.Data.TcpSyn.Embedder.Value = "ecntemporal"
			},
			isTiming: true,
		},
		channelTest{
			name: "UdpNormal",
			f1: func(conf *configData) {
				conf.Channel.Data.UdpNormal.DestinationPort.Value = 8090
				conf.Channel.Data.UdpNormal.OriginPort.Value = 8091
			},
			f2: func(conf *configData) {
				conf.Channel.Data.UdpNormal.DestinationPort.Value = 8091
				conf.Channel.Data.UdpNormal.OriginPort.Value = 8090
			},
		},
		channelTest{
			name: "UdpIP",
			f1: func(conf *configData) {
				conf.Channel.Data.UdpIP.FriendReceivePort.Value = 8090
				conf.Channel.Data.UdpIP.OriginReceivePort.Value = 8091

				conf.Processors = []processorConfig{
					processorConfig{
						Type: "Caesar", Data: defaultProcessor(),
					},
				}
			},
			f2: func(conf *configData) {
				conf.Channel.Data.UdpIP.FriendReceivePort.Value = 8091
				conf.Channel.Data.UdpIP.OriginReceivePort.Value = 8090

				conf.Processors = []processorConfig{
					processorConfig{
						Type: "Caesar", Data: defaultProcessor(),
					},
				}
			},
		},
	}

	messages := []string{"", "A", "Hello World!", "🍌", "🍌🍌🍌", "Hello\nNewline!"}
	for i := 0; i < 10; i++ {
		messages = append(messages, randomValidString(16))
	}

	// We have to use shorter messages for timing to prevent timeouts later
	// on and to make the tests run in reasonable time
	timingMessages := []string{"", "A", "Hello!", "🍌🍌🍌"}

	runMultiChannelWrite(t, cl, messages, timingMessages)
}

func randomValidString(maxLen int) string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	l := r.Int() & maxLen
	buf := []byte{}
	for i := 0; i < l; i++ {
		buf = append(buf, byte(r.Int()&0xFF))
	}
	return string(buf)
}
