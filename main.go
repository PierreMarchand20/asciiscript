package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"time"
)

var (
	ErrUnknownCtrl = errors.New("unknown control command")
	ErrNoArgs      = errors.New("no arguments given to command")
	ErrBadArg      = errors.New("invalid command argument")
)

func main() {

	// Flags
	var Exit bool
	flag.BoolVar(&Exit, "exit", false, "writing exit to end asciinema session instead of ctr-D")
	var Humanize bool
	flag.BoolVar(&Humanize, "humanize", false, "humanize typewrittingu using a normal law")
	var CtrlPrefix string
	flag.StringVar(&CtrlPrefix, "ctrl-prefix", "#$", "prefix for command in asciiscript file")
	var InputFile string
	flag.StringVar(&InputFile, "inputfile", "", "asciiscript input")
	var ArgsString string
	flag.StringVar(&ArgsString, "args", "", "asciinema arguments")

	var Wait int64
	flag.Int64Var(&Wait, "wait", 150, "time between commands")

	var Delay int64
	flag.Int64Var(&Delay, "delay", 80, "time between characters, if humanized, mean of the normal law")

	var StdDeviation int64
	flag.Int64Var(&StdDeviation, "stddeviation", 60, "standart deviation used in the normal law when humanized")

	flag.Parse()
	Args := strings.Fields(ArgsString)

	if InputFile == "" {
		log.Fatal("no script file provided")
	}

	if exec.Command("asciinema", "-h").Run() != nil {
		log.Fatal("can't find asciinema executable")
	}

	if Humanize {
		rand.Seed(time.Now().UnixNano())
	}

	s, err := NewScript(InputFile, Args, Wait, Delay, CtrlPrefix, Exit, Humanize, StdDeviation)
	if err != nil {
		log.Fatal("parsing script failed: ", err)
	}

	if err := s.Start(); err != nil {
		log.Fatal("couldn't start recording: ", err)
	}
	defer func() {
		if err := s.Stop(); err != nil {
			log.Fatal("couldn't stop recording: ", err)
		}
	}()

	s.Execute()
}

// Command is an action to be run.
type Command interface {
	Run(*Script)
}

// Shell is a shell command to execute.
type Shell struct {
	Cmd string
}

// NewShell creates a new Shell.
func NewShell(cmd string) Shell {
	if !strings.HasSuffix(cmd, "\n") {
		cmd += "\n"
	}
	return Shell{Cmd: cmd}
}

// Run runs the shell command.
func (s Shell) Run(sc *Script) {
	for _, c := range s.Cmd {
		if _, err := sc.Stdin.Write([]byte(fmt.Sprintf("%s", string(c)))); err != nil {
			os.Exit(1)
		}
		if sc.Humanize {
			r := int64(rand.NormFloat64()*float64(sc.StdDeviation) + float64(sc.Delay))
			time.Sleep(time.Millisecond * time.Duration(r))
		} else {
			time.Sleep(time.Millisecond * time.Duration(sc.Delay))
		}

	}
}

// Wait is a command to change the interval between commands.
type Wait struct {
	Duration int64
}

// NewWait creates a new Wait.
func NewWait(opts []string) (Wait, error) {
	if len(opts) == 0 {
		return Wait{}, ErrNoArgs
	}

	ms, err := strconv.ParseInt(strings.TrimSpace(opts[0]), 10, 64)
	if err != nil {
		return Wait{}, ErrBadArg
	}

	return Wait{Duration: ms}, nil
}

// Run changes the wait for subsequent commands.
func (w Wait) Run(s *Script) {
	s.Wait = w.Duration
}

// Delay is a command to change the typing speed of subsequent commands.
type Delay struct {
	Interval int64
}

// NewDelay creates a new Delay.
func NewDelay(opts []string) (Delay, error) {
	if len(opts) == 0 {
		return Delay{}, ErrNoArgs
	}

	ms, err := strconv.ParseInt(strings.TrimSpace(opts[0]), 10, 64)
	if err != nil {
		return Delay{}, ErrBadArg
	}

	return Delay{Interval: ms}, nil
}

// Run changes the typing speed for subsequent commands.
func (s Delay) Run(sc *Script) {
	sc.Delay = s.Interval
}

// NewCtrl creates a new control command.
func NewCtrl(cmd string) (Command, error) {
	tokens := strings.Split(cmd, " ")
	switch strings.TrimSpace(tokens[0]) {
	case "delay":
		return NewDelay(tokens[1:])
	case "wait":
		return NewWait(tokens[1:])
	default:
		return nil, ErrUnknownCtrl
	}
}

// Script is a shell script to be run and recorded by asciinema.
type Script struct {
	Args         []string
	Commands     []Command
	Delay        int64
	Wait         int64
	Cmd          *exec.Cmd
	Stdin        io.WriteCloser
	Stdout       io.ReadCloser
	Stderr       io.ReadCloser
	CtrlPrefix   string
	Humanize     bool
	StdDeviation int64
	Exit         bool
}

// NewScript parses a new Script from the script file at path.
func NewScript(path string, args []string, Wait int64, Delay int64, CtrlPrefix string, Exit bool, Humanize bool, StdDeviation int64) (*Script, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	s := &Script{
		Args:         args,
		Delay:        Delay,
		Wait:         Wait,
		CtrlPrefix:   CtrlPrefix,
		Exit:         Exit,
		Humanize:     Humanize,
		StdDeviation: StdDeviation,
	}

	lines := strings.Split(string(b), "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, s.CtrlPrefix) {
			ctrl, err := NewCtrl(strings.TrimSpace(line[len(s.CtrlPrefix):]))
			if err != nil {
				return nil, fmt.Errorf("%v (line %d)", err, i+1)
			}
			s.Commands = append(s.Commands, ctrl)
		} else {
			s.Commands = append(s.Commands, NewShell(line))
		}
	}

	return s, nil
}

// Start starts recording.
func (s *Script) Start() error {
	args := append([]string{"rec"}, s.Args...)
	fmt.Print(args)
	s.Cmd = exec.Command("asciinema", args...)
	var err error
	if s.Stdin, err = s.Cmd.StdinPipe(); err != nil {
		return err
	}
	if s.Stdout, err = s.Cmd.StdoutPipe(); err != nil {
		return err
	}
	if s.Stderr, err = s.Cmd.StderrPipe(); err != nil {
		return err
	}
	if err = s.Cmd.Start(); err != nil {
		return err
	}
	go echo(s.Stdout)
	go echo(s.Stderr)
	return nil
}

// Stop stops recording.
func (s *Script) Stop() error {
	defer s.Cmd.Wait()
	ending := "\004"
	if s.Exit {
		ending = "exit\n"
	}

	if _, err := s.Stdin.Write([]byte(ending)); err != nil {
		return err
	}
	if len(s.Args) == 0 || strings.HasPrefix(s.Args[0], "-") {
		s.endDialog()
	}
	return nil
}

func (s *Script) endDialog() {
	handler := make(chan os.Signal, 1)
	sig := make(chan os.Signal, 1)
	stdin := make(chan bool, 1)
	signal.Notify(handler, os.Interrupt)

	go func() {
		sig <- <-handler
		signal.Stop(handler)

	}()
	go func() {
		fmt.Scanln()
		stdin <- true
	}()

	select {
	case int := <-sig:
		s.Cmd.Process.Signal(int)
	case <-stdin:
		s.Stdin.Write([]byte{'\n'})
	}
}

// Execute runs the script's commands.
func (s *Script) Execute() {
	for _, c := range s.Commands {
		c.Run(s)

		time.Sleep(time.Millisecond * time.Duration(s.Wait))
	}
}

// echo prints out output continously.
func echo(r io.Reader) {
	buf := make([]byte, 1024)
	for {
		n, err := r.Read(buf)
		if err != nil {
			return
		}
		fmt.Print(string(buf[:n]))
	}
}
