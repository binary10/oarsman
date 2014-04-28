package s4

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/huin/goserial"
	"io"
	"io/ioutil"
	"log"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	UsbRequest                        = "USB"   // Application starting communication’s
	WrResponse                        = "_WR_"  // Hardware Type
	ExitRequest                       = "EXIT"  // Application is exiting
	OkResponse                        = "OK"    // Packet Accepted
	ErrorResponse                     = "ERROR" // Unknown packet
	PingResponse                      = "PING"  // Ping
	ResetRequest                      = "RESET" // Request the rowing computer to reset
	ModelInformationRequest           = "IV?"   // Request Model Information
	ModelInformationResponse          = "IV"    // Current model information
	ReadMemoryRequest                 = "IR"    // Read a memory location
	ReadMemoryResponse                = "ID"    // Value from a memory location
	StrokeStartResponse               = "SS"    // Start of stroke
	StrokeEndResponse                 = "SE"    // End of stroke
	PulseCountResponse                = "P"     // Pulse Count in the last 25mS
	DisplaySetIntensityRequest        = "DI"    // Display: Set Intensity
	DisplaySetDistanceRequest         = "DD"    // Display: Set Distance
	WorkoutSetDistanceRequest         = "WSI"   // Define a distance workout
	WorkoutSetDurationRequest         = "WSU"   // Define a duration workout
	IntervalWorkoutSetDistanceRequest = "WII"   // Define an interval distance workout
	IntervalWorkoutSetDurationRequest = "WIU"   // Define an interval duration workout
	AddIntervalWorkoutRequest         = "WIN"   // Add/End an interval to a workout
)

type Packet struct {
	cmd  string
	data []byte
}

func (p Packet) Bytes() []byte {
	var b bytes.Buffer
	b.Write([]byte(p.cmd))
	if p.data != nil {
		b.Write(p.data)
	}
	b.Write([]byte("\n"))
	return b.Bytes()
}

const (
	Unset             = 0
	ResetWaitingPing  = 1
	ResetPingReceived = 2
	WorkoutStarted    = 3
	WorkoutCompleted  = 4
	WorkoutExited     = 5
)

const (
	Meters = "1"
)

type Event struct {
	Time  int64
	Label string
	Value uint64
}

type Workout struct {
	workoutPacket Packet
	state         int
}

func NewWorkout(duration time.Duration, distanceMeters int64) Workout {
	// prepare workout instructions
	durationSeconds := int64(duration.Seconds())
	var workoutPacket Packet

	if durationSeconds > 0 {
		log.Printf("Starting single duration workout: %d seconds", durationSeconds)
		if durationSeconds >= 18000 {
			log.Fatalf("Workout time must be less than 18,000 seconds (was %d)", durationSeconds)
		}
		payload := fmt.Sprintf("%04X", durationSeconds)
		workoutPacket = Packet{cmd: WorkoutSetDurationRequest, data: []byte(payload)}
	} else if distanceMeters > 0 {
		log.Printf("Starting single distance workout: %d meters", distanceMeters)
		if distanceMeters >= 64000 {
			log.Fatalf("Workout distance must be less than 64,000 meters (was %d)", distanceMeters)
		}
		payload := Meters + fmt.Sprintf("%04X", distanceMeters)
		workoutPacket = Packet{cmd: WorkoutSetDistanceRequest, data: []byte(payload)}
	} else {
		log.Fatal("Undefined workout")
	}
	workout := Workout{workoutPacket: workoutPacket}
	return workout
}

type S4 struct {
	port    io.ReadWriteCloser
	scanner *bufio.Scanner
	workout Workout
	channel chan Event
	debug   bool
}

type EventCallbackFunc func(event chan Event)

func NewS4(callback EventCallbackFunc, debug bool) S4 {

	FindUsbSerialModem := func() string {
		contents, _ := ioutil.ReadDir("/dev")

		for _, f := range contents {
			if strings.Contains(f.Name(), "cu.usbmodem") {
				return "/dev/" + f.Name()
			}
		}

		return ""
	}

	name := FindUsbSerialModem()
	if len(name) == 0 {
		log.Fatal("S4 USB serial modem port not found")
	}

	c := &goserial.Config{Name: FindUsbSerialModem(), Baud: 115200, CRLFTranslate: true}
	p, err := goserial.OpenPort(c)
	if err != nil {
		log.Fatal(err)
	}

	channel := make(chan (Event))
	go callback(channel)

	s4 := S4{port: p, scanner: bufio.NewScanner(p), channel: channel, debug: debug}
	return s4
}

func (s4 *S4) Write(p Packet) {
	n, err := s4.port.Write(p.Bytes())
	if err != nil {
		log.Fatal(err)
	}
	if s4.debug {
		log.Printf("written %s (%d+1 bytes)", strings.TrimRight(string(p.Bytes()), "\n"), n-1)
	}
	time.Sleep(25 * time.Millisecond) // yield per spec
}

func (s4 *S4) Read() {
	for s4.scanner.Scan() {
		b := s4.scanner.Bytes()
		if len(b) > 0 {
			if s4.debug {
				log.Printf("read %s (%d+1 bytes)", string(b), len(b))
			}
			s4.OnPacketReceived(b)
			if s4.workout.state == WorkoutCompleted || s4.workout.state == WorkoutExited {
				return
			}
		}
	}

	if err := s4.scanner.Err(); err != nil {
		log.Fatal(err)
	}
}

func (s4 *S4) Run(workout Workout) {
	// send connection command and start listening
	s4.workout = workout
	s4.workout.state = Unset
	s4.Write(Packet{cmd: UsbRequest})
	s4.Read()
	s4.Exit()
}

func (s4 *S4) Exit() {
	if s4.workout.state != WorkoutExited {
		s4.Write(Packet{cmd: ExitRequest})
		s4.workout.state = WorkoutExited
	}
}

func (s4 *S4) OnPacketReceived(b []byte) {
	// responses can start with:
	// _ : _WR_
	// O : OK
	// E : ERROR
	// P : PING, P
	// S : SS, SE
	c := b[0]
	switch c {
	case '_':
		s4.WRHandler(b)
	case 'I':
		s4.InformationHandler(b)
	case 'O':
		s4.OKHandler()
	case 'E':
		s4.ErrorHandler()
	case 'P':
		s4.PingHandler(b)
	case 'S':
		s4.StrokeHandler(b)
	default:
		log.Printf("Unrecognized packet: %s", string(b))
	}
}

func (s4 *S4) WRHandler(b []byte) {
	s := string(b)
	if s == "_WR_" {
		s4.Write(Packet{cmd: ModelInformationRequest})
	} else {
		log.Fatalf("Unknown WaterRower init command %s", s)
	}
}

func (s4 *S4) ReadMemoryRequest(address string, size string) {
	cmd := ReadMemoryRequest + size
	data := []byte(address)
	s4.Write(Packet{cmd: cmd, data: data})
}

func (s4 *S4) OKHandler() {
	// ignore
}

func (s4 *S4) ErrorHandler() {
	if s4.workout.state == ResetPingReceived {
		s4.Write(Packet{cmd: ResetRequest})
		s4.workout.state = ResetWaitingPing
	}
}

func (s4 *S4) PingHandler(b []byte) {
	c := b[1]
	switch c {
	case 'I': // PING
		if s4.workout.state == ResetWaitingPing {
			s4.workout.state = ResetPingReceived
			s4.Write(s4.workout.workoutPacket)
		}
	default: // P
		// TODO implement P (pulse) packet
	}
}

type MemoryEntry struct {
	label string
	size  string
	base  int
}

var g_memorymap = map[string]MemoryEntry{
	"055": MemoryEntry{"total_distance_meters", "D", 16},
	"1A9": MemoryEntry{"stroke_rate", "S", 16},
	"088": MemoryEntry{"watts", "D", 16},
	"08A": MemoryEntry{"calories", "T", 16},
	"148": MemoryEntry{"speed_cm_s", "D", 16},
	"1A0": MemoryEntry{"heart_rate", "D", 16}}

func (s4 *S4) StrokeHandler(b []byte) {
	c := b[1]
	switch c {
	case 'S': // SS
		if s4.workout.state == ResetPingReceived {
			s4.workout.state = WorkoutStarted
			// these are the things we want captured from the S4
			for address, mmap := range g_memorymap {
				s4.ReadMemoryRequest(address, mmap.size)
			}
		}
		// TODO implement SS (stroke start) packet
	case 'E': // SE
		// TODO implement SE (stroke end) packet
	}
}

func (s4 *S4) InformationHandler(b []byte) {
	c1 := b[1]
	switch c1 {
	case 'V': // version
		// e.g. IV40210
		msg := string(b)
		log.Printf("WaterRower S%s %s.%s", msg[2:3], msg[3:5], msg[5:7])
		model, _ := strconv.ParseInt(msg[2:3], 0, 0)  // 4
		fwHigh, _ := strconv.ParseInt(msg[3:5], 0, 0) // 2
		fwLow, _ := strconv.ParseInt(msg[5:7], 0, 0)  // 10
		if model != 4 {
			log.Fatal("not an S4 monitor")
		}
		if fwHigh != 2 {
			log.Fatal("unsupported major S4 firmware version")
		}
		if fwLow != 10 {
			log.Fatal("unsupported minor S4 firmware version")
		}

		// we are ready to start workout
		s4.workout.state = ResetWaitingPing
		s4.Write(Packet{cmd: ResetRequest})

	case 'D': // memory value
		size := b[2]
		address := string(b[3:6])

		var l int
		switch size {
		case 'S':
			l = 1
		case 'D':
			l = 2
		case 'T':
			l = 3
		}
		v, err := strconv.ParseUint(string(b[6:(6+2*l)]), 16, 8*l)
		if err == nil {
			// we operate at 25ms resolution, so Unix() is too coarse
			// we use a syscall directly to avoid time parsing costs
			var tv syscall.Timeval
			syscall.Gettimeofday(&tv)
			millis := (int64(tv.Sec)*1e3 + int64(tv.Usec)/1e3)
			s4.channel <- Event{
				Time:  millis,
				Label: g_memorymap[address].label,
				Value: v}
			// we re-request the data
			if s4.workout.state == WorkoutStarted {
				s4.ReadMemoryRequest(address, string(size))
			}
		} else {
			log.Println("error parsing int: ", err)
		}
	}
}