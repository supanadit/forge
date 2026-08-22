package domain

// BuildStrategy enumerates the supported source build strategies.
type BuildStrategy string

const (
	// BuildStrategyConfigure uses ./configure && make (autotools).
	BuildStrategyConfigure BuildStrategy = "configure"
	// BuildStrategyAutogen uses autoreconf -fi && ./configure && make.
	BuildStrategyAutogen BuildStrategy = "autogen"
	// BuildStrategyCMake uses cmake && make.
	BuildStrategyCMake BuildStrategy = "cmake"
	// BuildStrategyMeson uses meson setup && ninja.
	BuildStrategyMeson BuildStrategy = "meson"
	// BuildStrategyMake uses make && make install directly.
	BuildStrategyMake BuildStrategy = "make"
	// BuildStrategyDetect auto-detects the strategy from the source tree.
	BuildStrategyDetect BuildStrategy = "detect"
)

// BuildSpec describes how to compile a source tree.
type BuildSpec struct {
	// Strategy is the build method; detect auto-selects.
	Strategy BuildStrategy
	// Prefix is the install prefix (e.g. /usr/local/pgsql).
	Prefix string
	// Flags are extra configure/cmake flags.
	Flags []string
	// Env are extra environment variables for the build commands.
	Env map[string]string
	// MakeFlags are extra make flags (e.g. ["-j8"]).
	MakeFlags []string
	// Jobs controls make parallelism (0 = auto).
	Jobs int
	// InstallTarget overrides the make target run on install (e.g. "altinstall"
	// for CPython). Empty runs "make install".
	InstallTarget string
	// Verbose streams build command output to the terminal when true.
	Verbose bool
}
