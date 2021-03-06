/*
 * Copyright 2018 Google Inc. All rights reserved.
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

package beamio

import (
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"kythe.io/kythe/go/platform/delimited"

	"github.com/apache/beam/sdks/go/pkg/beam"
	"github.com/apache/beam/sdks/go/pkg/beam/testing/ptest"
	"github.com/google/go-cmp/cmp"

	spb "kythe.io/kythe/proto/storage_go_proto"

	_ "github.com/apache/beam/sdks/go/pkg/beam/io/filesystem/local"
)

func TestReadEntries(t *testing.T) {
	entries := []*spb.Entry{{
		Source:   &spb.VName{Signature: "sig1"},
		FactName: "/kythe/fact/name",
	}, {
		Source:    &spb.VName{Signature: "sig1"},
		FactName:  "/kythe/fact/name2",
		FactValue: []byte("value"),
	}, {
		Source: &spb.VName{Signature: "sig2"},
		Target: &spb.VName{Signature: "sig1"},
	}, {
		EdgeKind: "/kythe/edge/kind",
	}}

	f, err := ioutil.TempFile("", "entries")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	wr := delimited.NewWriter(f)

	for _, e := range entries {
		if err := wr.PutProto(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	p, s := beam.NewPipelineWithRoot()

	coll := ReadEntries(s, f.Name())

	var found []*spb.Entry
	beam.ParDo(s, func(e *spb.Entry, emit func(*spb.Entry)) { found = append(found, e) }, coll)

	if err := ptest.Run(p); err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff(entries, found, ignoreProtoXXXFields); diff != "" {
		t.Fatalf("Diff found (-expected; +found):\n%s", diff)
	}
}

var ignoreProtoXXXFields = cmp.FilterPath(func(p cmp.Path) bool {
	for _, s := range p {
		if strings.HasPrefix(s.String(), ".XXX_") {
			return true
		}
	}
	return false
}, cmp.Ignore())
