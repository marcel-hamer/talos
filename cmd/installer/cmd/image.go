// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	stdruntime "runtime"
	"strings"

	"github.com/spf13/cobra"
	"github.com/talos-systems/go-cmd/pkg/cmd"

	"github.com/talos-systems/talos/cmd/installer/pkg"
	"github.com/talos-systems/talos/cmd/installer/pkg/install"
	"github.com/talos-systems/talos/cmd/installer/pkg/ova"
	"github.com/talos-systems/talos/cmd/installer/pkg/qemuimg"
	"github.com/talos-systems/talos/internal/app/machined/pkg/runtime"
	"github.com/talos-systems/talos/internal/app/machined/pkg/runtime/v1alpha1/platform"
	"github.com/talos-systems/talos/pkg/archiver"
	"github.com/talos-systems/talos/pkg/machinery/constants"
)

var (
	outputArg   string
	tarToStdout bool
)

// imageCmd represents the image command.
var imageCmd = &cobra.Command{
	Use:   "image",
	Short: "",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		if err := runImageCmd(); err != nil {
			log.Fatal(err)
		}
	},
}

func init() {
	imageCmd.Flags().StringVar(&outputArg, "output", "/out", "The output path")
	imageCmd.Flags().BoolVar(&tarToStdout, "tar-to-stdout", false, "Tar output and send to stdout")
	rootCmd.AddCommand(imageCmd)
}

//nolint: gocyclo
func runImageCmd() (err error) {
	p, err := platform.NewPlatform(options.Platform)
	if err != nil {
		return err
	}

	if err = os.MkdirAll(outputArg, 0o777); err != nil {
		return err
	}

	log.Printf("creating image for %s", p.Name())

	log.Print("creating RAW disk")

	img, err := pkg.CreateRawDisk()
	if err != nil {
		return err
	}

	log.Print("attaching loopback device ")

	if options.Disk, err = pkg.Loattach(img); err != nil {
		return err
	}

	defer func() {
		log.Println("detaching loopback device")

		if e := pkg.Lodetach(options.Disk); e != nil {
			log.Println(e)
		}
	}()

	if options.ConfigSource == "" {
		switch p.Name() {
		case "aws", "azure", "digital-ocean", "gcp":
			options.ConfigSource = constants.ConfigNone
		case "vmware":
			options.ConfigSource = constants.ConfigGuestInfo
		default:
		}
	}

	if err = install.Install(p, runtime.SequenceNoop, options); err != nil {
		return err
	}

	if err := finalize(p, img); err != nil {
		return err
	}

	if tarToStdout {
		if err := tarOutput(); err != nil {
			return err
		}
	}

	return nil
}

//nolint: gocyclo
func finalize(platform runtime.Platform, img string) (err error) {
	dir := filepath.Dir(img)

	file := filepath.Base(img)
	name := strings.TrimSuffix(file, filepath.Ext(file))

	switch platform.Name() {
	case "aws":
		if err = tar(fmt.Sprintf("aws-%s.tar.gz", stdruntime.GOARCH), file, dir); err != nil {
			return err
		}
	case "azure":
		file = name + ".vhd"

		if err = qemuimg.Convert("raw", "vpc", "subformat=fixed,force_size", img, filepath.Join(dir, file)); err != nil {
			return err
		}

		if err = tar(fmt.Sprintf("azure-%s.tar.gz", stdruntime.GOARCH), file, dir); err != nil {
			return err
		}
	case "digital-ocean":
		if err = tar(fmt.Sprintf("digital-ocean-%s.tar.gz", stdruntime.GOARCH), file, dir); err != nil {
			return err
		}
	case "gcp":
		if err = tar(fmt.Sprintf("gcp-%s.tar.gz", stdruntime.GOARCH), file, dir); err != nil {
			return err
		}
	case "openstack":
		if err = tar(fmt.Sprintf("openstack-%s.tar.gz", stdruntime.GOARCH), file, dir); err != nil {
			return err
		}
	case "vmware":
		if err = ova.CreateOVAFromRAW(name, img, outputArg); err != nil {
			return err
		}
	case "metal":
		if options.Board != constants.BoardNone {
			name := fmt.Sprintf("metal-%s-%s.img", options.Board, stdruntime.GOARCH)

			file = filepath.Join(outputArg, name)

			err = os.Rename(img, file)
			if err != nil {
				return err
			}

			log.Println("compressing image")

			if err = xz(file); err != nil {
				return err
			}

			break
		}

		name := fmt.Sprintf("metal-%s.tar.gz", stdruntime.GOARCH)

		if err = tar(name, file, dir); err != nil {
			return err
		}
	}

	return nil
}

func tar(filename, src, dir string) error {
	if _, err := cmd.Run("tar", "-czvf", filepath.Join(outputArg, filename), src, "-C", dir); err != nil {
		return err
	}

	return nil
}

func xz(filename string) error {
	if _, err := cmd.Run("xz", "-0", filename); err != nil {
		return err
	}

	return nil
}

func tarOutput() error {
	return archiver.TarGz(context.Background(), outputArg, os.Stdout)
}
