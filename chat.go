package main

import (
	"bufio"
	"encoding/gob"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"

	"github.com/anteater2/bitmesh/chord"
	"github.com/anteater2/bitmesh/dht"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s start/connect/listen [...]\n", os.Args[0])
		os.Exit(1)
	}
	switch os.Args[1] {
	case "start":
		handleStart()
	case "connect":
		handleConnect()
	case "listen":
		handleListen()
	default:
		fmt.Printf("Usage: %s start/connect/listen [...]\n", os.Args[0])
		os.Exit(1)
	}

}

func handleStart() {
	instroducer := os.Getenv("BITMESH_INTRODUCER")
	username := os.Getenv("BITMESH_USER")

	f, err := os.Open("/dev/null")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[chat FATAL] Cannot open log file: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()
	logWriter := bufio.NewWriter(f)
	log.SetOutput(logWriter)

	addr := getOutboundIP()

	// start chord node
	err = chord.Start(getOutboundIP(), 2001, 2000, 10)
	if err != nil {
		panic(err)
	}
	if addr != instroducer {
		err = chord.Join(instroducer + ":2001")
		if err != nil {
			fmt.Printf("[chat FATAL] Fail to join %s\n", instroducer+":2001")
			panic(err)
		}
	}

	// start DHT
	table, err := dht.New(instroducer+":2001", 3000, 10)
	table.Start()
	if err != nil {
		panic(err)
	}
	go func() {
		online := false
		for {
			err := table.Put(username, addr)
			if err != nil {
				online = false
				fmt.Printf("\n\n[chat WARNING] Fail to login: %v\n", err)
			} else if !online {
				online = true
				fmt.Printf("\n\n[chat INFO] You've sucessfully logged in as %s @ %s!\n", username, addr)
			}
			time.Sleep(3 * time.Second)
		}
	}()

	select {}
}

var busy = false

func handleConnect() {
	instroducer := os.Getenv("BITMESH_INTRODUCER")

	if len(os.Args) != 3 {
		fmt.Printf("Usage: %s connect <username>\n", os.Args[0])
		os.Exit(1)
	}

	username := os.Getenv("BITMESH_USER")
	if username == "" {
		fmt.Printf("BITMESH_USER not found in the environment.\n")
		os.Exit(1)
	}

	// start DHT
	table, err := dht.New(instroducer+":2001", 4000, 10)
	table.Start()
	if err != nil {
		panic(err)
	}

	name := os.Args[2]
	fmt.Printf("[chat INFO] Looking for %s...\n", name)
	raddr, err := table.Get(name)
	if err != nil {
		fmt.Printf("[chat WARNING] Fail to find %s: %v\n", name, err)
		fmt.Printf("[chat INFO] Stop connecting to %s\n", name)
		os.Exit(1)
	}
	fmt.Printf("[chat INFO] Found %s @ %s!\n", name, raddr)
	fmt.Printf("[chat INFO] Connecting to %s @ %s...\n", name, raddr)
	conn, err := net.Dial("tcp", raddr+":4000")
	if err != nil {
		fmt.Printf("[chat WARNING] Fail to connect %s @ %s: %v\n", name, raddr, err)
		fmt.Printf("[chat INFO] Stop connecting to %s\n", name)
		os.Exit(1)
	}
	createSession(conn, username, name).chatLoop(true)
}

func handleListen() {
	username := os.Getenv("BITMESH_USER")
	if username == "" {
		fmt.Printf("$BITMESH_CHAT_USERNAME not found. Have you start the client?\n")
		os.Exit(1)
	}

	l, err := net.Listen("tcp", ":4000")
	if err != nil {
		fmt.Printf("[FATAL] Fail to listen: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[chat INFO] Listening...\n")

	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Printf("\n[WARNING] Fail to accept: %v\n", err)
			continue
		}
		fmt.Printf("\n[INFO] New connection is comming from %s\n", conn.RemoteAddr().String())
		if !busy {
			fmt.Printf("[INFO] Do you want to connect? (y/n)\n")
			if getBoolChoice() {
				busy = true
				createSession(conn, username, "").chatLoop(false)
				busy = false
			} else {
				fmt.Printf("[INFO] Refused connection from %s\n\n", conn.RemoteAddr().String())
				conn.Close()
			}
		}
	}
}

type session struct {
	conn net.Conn
	enc  *gob.Encoder
	dec  *gob.Decoder
	me   string
	you  string
}

func createSession(conn net.Conn, me string, you string) *session {
	return &session{
		conn: conn,
		enc:  gob.NewEncoder(conn),
		dec:  gob.NewDecoder(conn),
		me:   me,
		you:  you,
	}
}

func (s *session) chatLoop(isInitiator bool) {
	defer s.conn.Close()

	if isInitiator {
		// send the username
		err := s.enc.Encode(&s.me)
		if err == io.EOF {
			s.conn.Close()
			return
		}
		if err != nil {
			fmt.Printf("[chat WARNING] Fail to send: %v\n", err)
			return
		}
	} else {
		// get the username of the other side
		err := s.dec.Decode(&s.you)
		if err == io.EOF {
			s.conn.Close()
			return
		}
		if err != nil {
			fmt.Printf("[chat WARNING] Fail to receive: %v\n", err)
			return
		}
	}

	raddr, _, _ := net.SplitHostPort(s.conn.RemoteAddr().String())

	fmt.Printf("[chat INFO] Now you are connected to %s@%s!\n", s.you, raddr)

	exit := make(chan struct{}, 1)

	// receiver
	go func() {
		defer func() { exit <- struct{}{} }()
		for {
			var msg string
			err := s.dec.Decode(&msg)
			if err == io.EOF {
				return
			}
			if err != nil {
				fmt.Printf("[chat WARNING] Fail to receive: %v\n", err)
				return
			}
			fmt.Printf("%s@%s> %s", s.you, raddr, msg)
		}
	}()

	// sender
	go func() {
		defer func() { exit <- struct{}{} }()
		input := bufio.NewReader(os.Stdin)
		for {
			line, err := input.ReadString('\n')
			if err != nil {
				return
			}
			err = s.enc.Encode(line)
			if err == io.EOF {
				return
			}
			if err != nil {
				fmt.Printf("[chat WARNING] Fail to send: %v\n", err)
				return
			}
		}
	}()
	select {
	case <-exit:
		break
	}
}

// getOutboundIP gets preferred outbound IP of this machine using a filthy hack
// The connection should not actually require the Google DNS service (the 8.8.8.8),
// but by creating it we can see what our preferred IP is.
func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

func getBoolChoice() bool {
	input := bufio.NewReader(os.Stdin)
	line, _ := input.ReadString('\n')
	return line == "y\n"
}
