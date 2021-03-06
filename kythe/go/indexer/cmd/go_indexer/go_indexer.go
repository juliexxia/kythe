/*
 * Copyright 2016 Google Inc. All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Program go_indexer implements a Kythe indexer for the Go language.  Input is
// read from one or more .kzip, .kindex, or index pack paths.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"kythe.io/kythe/go/indexer"
	"kythe.io/kythe/go/platform/delimited"
	"kythe.io/kythe/go/platform/indexpack"
	"kythe.io/kythe/go/platform/kindex"
	"kythe.io/kythe/go/platform/kzip"
	"kythe.io/kythe/go/platform/vfs"
	"kythe.io/kythe/go/util/metadata"

	apb "kythe.io/kythe/proto/analysis_go_proto"
	spb "kythe.io/kythe/proto/storage_go_proto"
)

var (
	doIndexPack = flag.Bool("indexpack", false, "Treat arguments as index pack directories")
	doZipPack   = flag.Bool("zip", false, "Treat arguments as zipped indexpack files (implies -indexpack)")
	doJSON      = flag.Bool("json", false, "Write output as JSON")
	doLibNodes  = flag.Bool("libnodes", false, "Emit nodes for standard library packages")
	doCodeFacts = flag.Bool("code", false, "Emit code facts containing MarkedSource markup")
	metaSuffix  = flag.String("meta", "", "If set, treat files with this suffix as JSON linkage metadata")
	docBase     = flag.String("docbase", "http://godoc.org", "If set, use as the base URL for godoc links")
	verbose     = flag.Bool("verbose", false, "Emit verbose log information")
	contOnErr   = flag.Bool("continue", false, "Log errors encountered during analysis but do not exit unsuccessfully")

	writeEntry func(context.Context, *spb.Entry) error
	docURL     *url.URL
)

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: %s [options] <path>...

Generate Kythe graph data for the compilations stored in .kzip, .kindex, or
index packs named by the path arguments. Output is written to stdout.

If --indexpack is set, the paths are treated as index packs. If --zip is set,
the index packs are treaed as ZIP files. Otherwise, the paths must end in .kzip
or .kindex and will be decoded accordingly.

By default, the output is a delimited stream of wire-format Kythe Entry
protobuf messages. With the --json flag, output is instead a stream of
undelimited JSON messages.

Options:
`, filepath.Base(os.Args[0]))

		flag.PrintDefaults()
	}
}

func main() {
	flag.Parse()

	if flag.NArg() == 0 {
		log.Fatal("No input paths were specified to index")
	}
	if *doJSON {
		enc := json.NewEncoder(os.Stdout)
		writeEntry = func(_ context.Context, entry *spb.Entry) error {
			return enc.Encode(entry)
		}
	} else {
		rw := delimited.NewWriter(os.Stdout)
		writeEntry = func(_ context.Context, entry *spb.Entry) error {
			return rw.PutProto(entry)
		}
	}
	if *docBase != "" {
		u, err := url.Parse(*docBase)
		if err != nil {
			log.Fatalf("Invalid doc base URL: %v", err)
		}
		docURL = u
	}

	ctx := context.Background()
	for _, path := range flag.Args() {
		if err := visitPath(ctx, path, func(ctx context.Context, unit *apb.CompilationUnit, f indexer.Fetcher) error {
			err := indexGo(ctx, unit, f)
			if err != nil && *contOnErr {
				log.Printf("Continuing after error: %v", err)
				return nil
			}
			return err
		}); err != nil {
			log.Fatalf("Error indexing %q: %v", path, err)
		}
	}
}

// checkMetadata checks whether ri denotes a metadata file according to the
// setting of the -meta flag, and if so loads the corresponding ruleset.
func checkMetadata(ri *apb.CompilationUnit_FileInput, f indexer.Fetcher) (*indexer.Ruleset, error) {
	if *metaSuffix == "" || !strings.HasSuffix(ri.Info.GetPath(), *metaSuffix) {
		return nil, nil // nothing to do
	}
	bits, err := f.Fetch(ri.Info.GetPath(), ri.Info.GetDigest())
	if err != nil {
		return nil, fmt.Errorf("reading metadata file: %v", err)
	}
	rules, err := metadata.Parse(bytes.NewReader(bits))
	if err != nil {
		return nil, err
	}
	return &indexer.Ruleset{
		Path:  strings.TrimSuffix(ri.Info.GetPath(), *metaSuffix),
		Rules: rules,
	}, nil
}

// indexGo is a visitFunc that invokes the Kythe Go indexer on unit.
func indexGo(ctx context.Context, unit *apb.CompilationUnit, f indexer.Fetcher) error {
	pi, err := indexer.Resolve(unit, f, &indexer.ResolveOptions{
		Info:       indexer.XRefTypeInfo(),
		CheckRules: checkMetadata,
	})
	if err != nil {
		return err
	}
	if *verbose {
		log.Printf("Finished resolving compilation: %s", pi.String())
	}
	return pi.Emit(ctx, writeEntry, &indexer.EmitOptions{
		EmitStandardLibs: *doLibNodes,
		EmitMarkedSource: *doCodeFacts,
		EmitLinkages:     *metaSuffix != "",
		DocBase:          docURL,
	})
}

type visitFunc func(context.Context, *apb.CompilationUnit, indexer.Fetcher) error

// visitPath invokes visit for each compilation denoted by path, which is
// either a .kindex file (with a single compilation) or an index pack.
func visitPath(ctx context.Context, path string, visit visitFunc) error {
	if *doIndexPack || *doZipPack {
		return visitIndexPack(ctx, path, visit)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	switch ext := filepath.Ext(path); ext {
	case ".kindex":
		idx, err := kindex.New(f)
		if err != nil {
			return fmt.Errorf("reading .kindex: %v", err)
		}
		return visit(ctx, idx.Proto, idx)
	case ".kzip":
		return kzip.Scan(f, func(r *kzip.Reader, unit *kzip.Unit) error {
			return visit(ctx, unit.Proto, kzipFetcher{r})
		})

	default:
		return fmt.Errorf("unknown file extension %q", ext)
	}
}

// visitIndexPack invokes visit for each Kythe compilation in the index pack at
// path. Any error returned by the visitor terminates the scan.
func visitIndexPack(ctx context.Context, path string, visit visitFunc) error {
	pack, err := openPack(ctx, path)
	if err != nil {
		return fmt.Errorf("opening indexpack: %v", err)
	}
	return pack.ReadUnits(ctx, "kythe", func(_ string, msg interface{}) error {
		return visit(ctx, msg.(*apb.CompilationUnit), pack.Fetcher(ctx))
	})
}

func openPack(ctx context.Context, path string) (*indexpack.Archive, error) {
	utype := indexpack.UnitType((*apb.CompilationUnit)(nil))
	if *doZipPack {
		fi, err := vfs.Stat(ctx, path)
		if err != nil {
			return nil, err
		} else if !fi.Mode().IsRegular() {
			return nil, fmt.Errorf("invalid zip file path %q: %v", path, err)
		}
		f, err := vfs.Open(ctx, path)
		if err != nil {
			return nil, err
		}
		return indexpack.OpenZip(ctx, f.(io.ReaderAt), fi.Size(), utype)
	}
	return indexpack.Open(ctx, path, utype)
}

type kzipFetcher struct{ r *kzip.Reader }

// Fetch implements the analysis.Fetcher interface. Only the digest is used in
// this implementation, the path is ignored.
func (k kzipFetcher) Fetch(_, digest string) ([]byte, error) {
	return k.r.ReadAll(digest)
}
