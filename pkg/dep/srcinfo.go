package dep

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	aurc "github.com/Jguer/aur"
	gosrc "github.com/Morganamilo/go-srcinfo"
	"github.com/leonelquinteros/gotext"

	"github.com/Jguer/yay/v12/pkg/dep/topo"
	"github.com/Jguer/yay/v12/pkg/settings/exe"
)

var ErrNoBuildFiles = errors.New(gotext.Get("cannot find PKGBUILD and .SRCINFO in directory"))

func (g *Grapher) GraphFromSrcInfoDirs(ctx context.Context, graph *topo.Graph[string, *InstallInfo],
	srcInfosDirs []string,
) (*topo.Graph[string, *InstallInfo], error) {
	if graph == nil {
		graph = NewGraph()
	}

	srcInfos := map[string]*gosrc.Srcinfo{}
	for _, targetDir := range srcInfosDirs {
		if err := srcinfoExists(ctx, g.cmdBuilder, targetDir); err != nil {
			return nil, err
		}

		pkgbuild, err := gosrc.ParseFile(filepath.Join(targetDir, ".SRCINFO"))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", gotext.Get("failed to parse .SRCINFO"), err)
		}

		srcInfos[targetDir] = pkgbuild
	}

	aurPkgsAdded := []*aurc.Pkg{}
	for pkgBuildDir, pkgbuild := range srcInfos {
		pkgBuildDir := pkgBuildDir

		aurPkgs, err := makeAURPKGFromSrcinfo(g.dbExecutor, pkgbuild)
		if err != nil {
			return nil, err
		}

		if len(aurPkgs) > 1 {
			var errPick error
			aurPkgs, errPick = g.pickSrcInfoPkgs(aurPkgs)
			if errPick != nil {
				return nil, errPick
			}
		}

		for _, pkg := range aurPkgs {
			pkg := pkg

			reason := Explicit
			if pkg := g.dbExecutor.LocalPackage(pkg.Name); pkg != nil {
				reason = Reason(pkg.Reason())
			}

			graph.AddNode(pkg.Name)

			g.addAurPkgProvides(pkg, graph)

			validateAndSetNodeInfo(graph, pkg.Name, &topo.NodeInfo[*InstallInfo]{
				Color:      colorMap[reason],
				Background: bgColorMap[AUR],
				Value: &InstallInfo{
					Source:      SrcInfo,
					Reason:      reason,
					SrcinfoPath: &pkgBuildDir,
					AURBase:     &pkg.PackageBase,
					Version:     pkg.Version,
				},
			})
		}

		aurPkgsAdded = append(aurPkgsAdded, aurPkgs...)
	}

	g.AddDepsForPkgs(ctx, aurPkgsAdded, graph)

	return graph, nil
}

func srcinfoExists(ctx context.Context,
	cmdBuilder exe.ICmdBuilder, targetDir string,
) error {
	srcInfoDir := filepath.Join(targetDir, ".SRCINFO")
	pkgbuildDir := filepath.Join(targetDir, "PKGBUILD")
	if _, err := os.Stat(srcInfoDir); err == nil {
		if _, err := os.Stat(pkgbuildDir); err == nil {
			return nil
		}
	}

	if _, err := os.Stat(pkgbuildDir); err == nil {
		// run makepkg to generate .SRCINFO
		srcinfo, stderr, err := cmdBuilder.Capture(cmdBuilder.BuildMakepkgCmd(ctx, targetDir, "--printsrcinfo"))
		if err != nil {
			return fmt.Errorf("unable to generate .SRCINFO: %w - %s", err, stderr)
		}

		if err := os.WriteFile(srcInfoDir, []byte(srcinfo), 0o600); err != nil {
			return fmt.Errorf("unable to write .SRCINFO: %w", err)
		}

		return nil
	}

	return fmt.Errorf("%w: %s", ErrNoBuildFiles, targetDir)
}