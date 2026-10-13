package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/jeremyjaymes/weeklog/internal/config"
	"github.com/jeremyjaymes/weeklog/internal/source"
)

var errDoctor = errors.New("doctor failed")

func newDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check paths, permissions, and plugin protocol",
		Long: `Check the config path, the plugin directory, and protocol describe for every source.

External plugins are executables named weeklog-source-<name> on PATH or in the
plugin directory. doctor does not resolve secrets and does not print source settings.
A missing config file is a warning. A plugin that fails describe is an error.
--timeout limits each plugin.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			timeout, err := cmd.Flags().GetDuration("timeout")
			if err != nil {
				return err
			}
			if timeout <= 0 {
				return fmt.Errorf("timeout must be positive")
			}
			flag, err := cmd.Root().PersistentFlags().GetString("config")
			if err != nil {
				return err
			}
			opts, err := doctorOptions(flag)
			if err != nil {
				return err
			}
			reg := newSourceRegistry()
			reg.SetTimeout(timeout)
			report, err := reg.Doctor(cmdContext(cmd), opts)
			if err != nil {
				return err
			}
			if _, err := io.WriteString(cmd.OutOrStdout(), report.Text()); err != nil {
				return err
			}
			if !report.OK() {
				return errDoctor
			}
			return nil
		},
	}
	cmd.Flags().Duration("timeout", source.DefaultTimeout, "time limit for each plugin describe")
	return cmd
}

// doctorOptions loads config for names only. Secret references are not resolved.
func doctorOptions(flag string) (source.DoctorOptions, error) {
	path, err := config.ResolveConfigPath(flag)
	if err != nil {
		return source.DoctorOptions{ConfigErr: err}, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return source.DoctorOptions{ConfigPath: path, ConfigMissing: true}, nil
		}
		return source.DoctorOptions{ConfigPath: path, ConfigErr: errors.New("cannot read config")}, nil
	}
	if info.IsDir() {
		return source.DoctorOptions{ConfigPath: path, ConfigErr: errors.New("config path is a directory")}, nil
	}
	cfg, err := config.Load(path)
	if err != nil {
		// Load errors can quote the file. Keep the doctor report free of that text.
		return source.DoctorOptions{ConfigPath: path, ConfigErr: errors.New("cannot parse config")}, nil
	}
	configured := make([]source.Configured, len(cfg.Sources))
	for i, src := range cfg.Sources {
		configured[i] = source.Configured{Index: i, Name: src.Type}
	}
	return source.DoctorOptions{ConfigPath: path, Configured: configured}, nil
}
