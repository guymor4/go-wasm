//go:build js
// +build js

package process

import (
	"github.com/hack-pad/hackpad/internal/log"
	"github.com/hack-pad/hackpad/internal/promise"
	"io"
	"syscall/js"

	"github.com/hack-pad/hackpad/internal/fs"
	"github.com/hack-pad/hackpad/internal/interop"
	"github.com/hack-pad/hackpad/internal/process"
	"github.com/pkg/errors"
)

func spawn(args []js.Value) (interface{}, error) {
	if len(args) == 0 {
		return nil, errors.Errorf("Invalid number of args, expected command name: %v", args)
	}

	command := args[0].String()
	argv := []string{command}
	if len(args) >= 2 {
		if args[1].Type() != js.TypeObject || args[1].Get("length").IsUndefined() {
			return nil, errors.New("Second arg must be an array of arguments")
		}
		length := args[1].Length()
		for i := 0; i < length; i++ {
			argv = append(argv, args[1].Index(i).String())
		}
	}

	procAttr := &process.ProcAttr{}
	if len(args) >= 3 {
		argv[0], procAttr = parseProcAttr(command, args[2])
	}
	return Spawn(command, argv, procAttr)
}

func execCommand(args []js.Value) (interface{}, error) {
	if len(args) != 2 {
		return nil, errors.Errorf("Invalid number of args, expected 2: command, args: got %v", args)
	}

	command := args[0].String()
	// args[1] is an array of arguments
	// argv must contain the command as the first argument
	argv := []string{command}
	if args[1].Type() != js.TypeObject || args[1].Get("length").IsUndefined() {
		return nil, errors.New("Second arg must be an array of arguments")
	}
	length := args[1].Length()
	for i := 0; i < length; i++ {
		argv = append(argv, args[1].Index(i).String())
	}

	return Exec(command, argv), nil
}

type jsWrapper interface {
	JSValue() js.Value
}

func Spawn(command string, args []string, attr *process.ProcAttr) (js.Value, error) {
	p, err := process.New(command, args, attr)
	if err != nil {
		return js.Value{}, err
	}
	return p.(jsWrapper).JSValue(), p.Start()
}

func Exec(command string, args []string) interface{} {
	resolve, reject, prom := promise.New()

	files := process.Current().Files()
	stdinR, _ := pipe(files)
	stdoutR, stdoutW := pipe(files)
	stderrR, stderrW := pipe(files)

	proc, err := process.New(command, args, &process.ProcAttr{
		Files: []fs.Attr{
			{FID: stdinR},
			{FID: stdoutW},
			{FID: stderrW},
		},
	})

	log.Print("Starting ", args)
	if err != nil {
		reject(interop.WrapAsJSError(err, "Failed to spawn process"))
		return prom.JSValue()
	}

	err = proc.Start()
	if err != nil {
		reject(interop.WrapAsJSError(err, "Failed to spawn process"))
		return prom.JSValue()
	}

	outReader, err := files.RawFID(stdoutR)
	if err != nil {
		reject(interop.WrapAsJSError(err, "Failed to spawn process"))
		return prom.JSValue()
	}
	errReader, err := files.RawFID(stderrR)
	if err != nil {
		reject(interop.WrapAsJSError(err, "Failed to spawn process"))
		return prom.JSValue()
	}

	var output string
	var outputErr string
	onWrite := func(input string) {
		output += input
	}
	onWriteErr := func(input string) {
		outputErr += input
	}

	go readOutputPipes(outReader, onWrite)
	go readOutputPipes(errReader, onWriteErr)

	// Resolve the promise when the process exits
	go func() {
		exitCode, _ := proc.Wait()
		resolve(js.ValueOf(map[string]interface{}{
			"stdout":   output,
			"stderr":   outputErr,
			"pid":      proc.PID().JSValue(),
			"exitCode": exitCode,
		}))
	}()

	return prom.JSValue()
}

func readOutputPipes(r io.Reader, onWrite func(input string)) {
	buf := make([]byte, 1)
	for {
		_, err := r.Read(buf)
		switch err {
		case nil:
			onWrite(string(buf[:]))
		case io.EOF:
			log.Debug("Output pipe closed")
			return
		default:
			log.Error("Failed to write to terminal:", err)
		}
	}
}

func parseProcAttr(defaultCommand string, value js.Value) (argv0 string, attr *process.ProcAttr) {
	argv0 = defaultCommand
	attr = &process.ProcAttr{}
	if dir := value.Get("cwd"); dir.Truthy() {
		attr.Dir = dir.String()
	}
	if env := value.Get("env"); env.Truthy() {
		attr.Env = make(map[string]string)
		for name, prop := range interop.Entries(env) {
			attr.Env[name] = prop.String()
		}
	}

	if stdio := value.Get("stdio"); stdio.Truthy() {
		length := stdio.Length()
		for i := 0; i < length; i++ {
			file := stdio.Index(i)
			switch file.Type() {
			case js.TypeNumber:
				fd := fs.FID(file.Int())
				attr.Files = append(attr.Files, fs.Attr{FID: fd})
			case js.TypeString:
				switch file.String() {
				case "ignore":
					attr.Files = append(attr.Files, fs.Attr{Ignore: true})
				case "inherit":
					attr.Files = append(attr.Files, fs.Attr{FID: fs.FID(i)})
				case "pipe":
					attr.Files = append(attr.Files, fs.Attr{Pipe: true})
				}
			}
		}
	}

	if jsArgv0 := value.Get("argv0"); jsArgv0.Truthy() {
		argv0 = jsArgv0.String()
	}

	return
}

func pipe(files *fs.FileDescriptors) (r, w fs.FID) {
	p := files.Pipe()
	return p[0], p[1]
}
