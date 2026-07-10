package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/CUG-CS-632/CampusLink/internal/srun"
)

const (
	maxPasswordBytes  = 1 << 20
	maxTimeoutSeconds = 3600
)

var (
	version = "dev"
	commit  = ""
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	defaultConfig := srun.DefaultConfig()
	timeoutDefault, timeoutEnvErr := timeoutFromEnv(int(defaultConfig.Timeout / time.Second))

	var (
		username      string
		password      string
		ip            string
		baseURL       string
		acID          string
		timeoutSecs   int
		printJSON     bool
		passwordStdin bool
		printVersion  bool
	)

	flags := flag.NewFlagSet("campuslink", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&username, "u", os.Getenv("SRUN_USERNAME"), "campus network username")
	flags.StringVar(&username, "username", os.Getenv("SRUN_USERNAME"), "campus network username")
	flags.StringVar(&password, "p", os.Getenv("SRUN_PASSWORD"), "campus network password")
	flags.StringVar(&password, "password", os.Getenv("SRUN_PASSWORD"), "campus network password")
	flags.BoolVar(&passwordStdin, "password-stdin", false, "read the campus network password from standard input")
	flags.StringVar(&ip, "ip", os.Getenv("SRUN_IP"), "client IP; auto-discovered when empty")
	flags.StringVar(&baseURL, "host", portalBaseURL(defaultConfig.BaseURL), "SRun portal host or base URL")
	flags.StringVar(&baseURL, "base-url", portalBaseURL(defaultConfig.BaseURL), "SRun portal base URL; http and https are supported")
	flags.StringVar(&acID, "ac-id", getenv("SRUN_AC_ID", defaultConfig.ACID), "SRun AC ID")
	flags.IntVar(&timeoutSecs, "timeout", timeoutDefault, "HTTP timeout in seconds (1-3600)")
	flags.BoolVar(&printJSON, "json", false, "print raw portal response as JSON")
	flags.BoolVar(&printVersion, "version", false, "print version and exit")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "error: unexpected arguments: %s\n", strings.Join(flags.Args(), " "))
		return 2
	}
	if printVersion {
		fmt.Fprintf(stdout, "campuslink %s\n", currentVersion())
		return 0
	}
	if timeoutEnvErr != nil && !flagWasSet(flags, "timeout") {
		fmt.Fprintln(stderr, "error:", timeoutEnvErr)
		return 2
	}
	if timeoutSecs < 1 || timeoutSecs > maxTimeoutSeconds {
		fmt.Fprintf(stderr, "error: timeout must be between 1 and %d seconds\n", maxTimeoutSeconds)
		return 2
	}
	if passwordStdin {
		if password != "" {
			fmt.Fprintln(stderr, "error: --password-stdin cannot be combined with -p, --password, or SRUN_PASSWORD")
			return 2
		}
		var err error
		password, err = readPassword(stdin)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 2
		}
	}

	config := defaultConfig
	config.BaseURL = baseURL
	config.ACID = acID
	config.Timeout = time.Duration(timeoutSecs) * time.Second

	client, err := srun.NewClient(config)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 2
	}

	result, err := client.Login(username, password, ip)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	ok := srun.OK(result)
	if printJSON {
		encoded, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		fmt.Fprintln(stdout, string(encoded))
		if ok {
			return 0
		}
		return 1
	}

	if ok {
		fmt.Fprintln(stdout, "login ok")
		return 0
	}

	message := firstString(result, "error_msg", "message")
	if message == "" {
		encoded, _ := json.Marshal(result)
		message = string(encoded)
	}
	fmt.Fprintln(stderr, "login failed:", message)
	return 1
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func portalBaseURL(fallback string) string {
	if value := os.Getenv("SRUN_BASE_URL"); value != "" {
		return value
	}
	return getenv("SRUN_HOST", fallback)
}

func timeoutFromEnv(fallback int) (int, error) {
	value := os.Getenv("SRUN_TIMEOUT")
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback, fmt.Errorf("invalid SRUN_TIMEOUT %q: must be an integer number of seconds", value)
	}
	return parsed, nil
}

func flagWasSet(flags *flag.FlagSet, name string) bool {
	set := false
	flags.Visit(func(current *flag.Flag) {
		if current.Name == name {
			set = true
		}
	})
	return set
}

func readPassword(input io.Reader) (string, error) {
	reader := bufio.NewReader(io.LimitReader(input, maxPasswordBytes+1))
	value, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read password from standard input: %w", err)
	}
	if len(value) > maxPasswordBytes {
		return "", errors.New("password from standard input is too large")
	}
	value = strings.TrimSuffix(value, "\n")
	value = strings.TrimSuffix(value, "\r")
	if value == "" {
		return "", errors.New("password from standard input is empty")
	}
	return value, nil
}

func currentVersion() string {
	buildVersion := version
	if buildVersion == "" {
		buildVersion = "dev"
	}
	buildCommit := commit
	modified := false
	if info, ok := debug.ReadBuildInfo(); ok {
		if buildVersion == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
			buildVersion = strings.TrimSuffix(info.Main.Version, "+dirty")
		}
		if buildCommit == "" {
			for _, setting := range info.Settings {
				switch setting.Key {
				case "vcs.revision":
					buildCommit = setting.Value
				case "vcs.modified":
					modified = setting.Value == "true"
				}
			}
		}
	}
	if buildCommit == "" {
		return buildVersion
	}
	if len(buildCommit) > 12 {
		buildCommit = buildCommit[:12]
	}
	if modified {
		buildCommit += "-dirty"
	}
	return fmt.Sprintf("%s (commit %s)", buildVersion, buildCommit)
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}
