package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// ---------------------------------------------------------------------------
// Command line
// ---------------------------------------------------------------------------

// opts is the parsed command line.
type opts struct {
	configPath string
	model      string
	baseURL    string
	yes        bool
	version    bool

	cont     bool   // --continue: resume the newest session of this project
	resume   string // --resume <id>: resume the session with this id (prefix allowed)
	pick     bool   // -r / --resume without an id: choose from the project's sessions
	sessions bool   // --sessions: print the project's sessions and exit
	noSave   bool   // --no-save: do not write a SessionFile
	traceOff bool   // --trace-off: do not write a Trace

	trace   bool   // subcommand "swarg trace [id]"
	traceID string // its optional session id (prefix allowed)
}

// errHelp is returned by parseArgs after the usage text was printed for -h.
var errHelp = flag.ErrHelp

// parseArgs parses the command line (without the program name). Usage and
// parse errors are printed to out by the flag package; callers map the error to exit 2.
func parseArgs(args []string, out io.Writer) (opts, error) {
	var o opts
	if len(args) > 0 && args[0] == "trace" {
		if len(args) > 2 {
			return o, errors.New("usage: swarg trace [id]")
		}
		o.trace = true
		if len(args) == 2 {
			o.traceID = args[1]
		}
		return o, nil
	}

	fs := flag.NewFlagSet("swarg", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&o.configPath, "config", "", "config file (default: config.json in the swarg config dir)")
	fs.StringVar(&o.model, "model", "", "model name of the active provider")
	fs.StringVar(&o.baseURL, "base-url", "", "base URL of the active provider")
	fs.BoolVar(&o.yes, "yes", false, "approve every tool call (plain mode, sandbox runs)")
	fs.BoolVar(&o.version, "version", false, "print the version and exit")
	fs.BoolVar(&o.cont, "continue", false, "resume the newest session of this project")
	fs.BoolVar(&o.cont, "c", false, "short for --continue")
	fs.StringVar(&o.resume, "resume", "", "resume the session with this `id` (a prefix is enough); without an id: choose from a list")
	fs.StringVar(&o.resume, "r", "", "short for --resume")
	fs.BoolVar(&o.sessions, "sessions", false, "list the sessions of this project and exit")
	fs.BoolVar(&o.noSave, "no-save", false, "do not save this session")
	fs.BoolVar(&o.traceOff, "trace-off", false, "do not write the debug trace")
	args, o.pick = scanPick(args)
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if fs.NArg() > 0 {
		return o, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	resumeSet := false
	fs.Visit(func(f *flag.Flag) { resumeSet = resumeSet || f.Name == "resume" || f.Name == "r" })
	if resumeSet && (o.resume == "" || strings.HasPrefix(o.resume, "-")) {
		return o, errors.New("--resume needs a session id (see swarg --sessions)")
	}
	if o.cont && (resumeSet || o.pick) {
		return o, errors.New("--continue and --resume cannot be used together")
	}
	if o.pick && resumeSet {
		return o, errors.New("--resume given twice: with and without an id")
	}
	return o, nil
}

// scanPick finds -r / --resume (also -resume) WITHOUT an id: as the last argument or followed
// by a flag. The flag package has no optional values, so those occurrences are removed and
// reported as pick. -r ID and --resume=ID are left for the flag package.
func scanPick(args []string) (rest []string, pick bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		if (a == "-r" || a == "--resume" || a == "-resume") && (i == len(args)-1 || strings.HasPrefix(args[i+1], "-")) {
			pick = true
			continue
		}
		rest = append(rest, a)
		// a flag that takes a value swallows the next arg, even if it looks like -r
		switch a {
		case "-config", "--config", "-model", "--model", "-base-url", "--base-url", "-r", "--resume", "-resume":
			if i+1 < len(args) {
				i++
				rest = append(rest, args[i])
			}
		}
	}
	return rest, pick
}

// ---------------------------------------------------------------------------
// Provider override: flags, then the environment
// ---------------------------------------------------------------------------

// providerOverride is the model and base URL to apply over the loaded config: the --model /
// --base-url flags when given, else the SWARG_MODEL / SWARG_BASE_URL environment variables.
// The flags win, so a command line beats the environment the sandbox sets. Either may be
// empty, which Override leaves untouched.
func (o opts) providerOverride() (model, baseURL string) {
	model, baseURL = o.model, o.baseURL
	if model == "" {
		model = os.Getenv("SWARG_MODEL")
	}
	if baseURL == "" {
		baseURL = os.Getenv("SWARG_BASE_URL")
	}
	return model, baseURL
}
