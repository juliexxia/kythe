load("//tools:build_rules/shims.bzl", "go_test", "go_library")

package(default_visibility = ["//kythe:default_visibility"])

go_library(
    name = "paths",
    srcs = ["paths.go"],
    deps = [
        "//kythe/go/services/xrefs",
        "//kythe/go/serving/xrefs/assemble",
        "//kythe/go/util/reduce",
        "//kythe/go/util/schema/edges",
        "//kythe/go/util/schema/facts",
        "//kythe/go/util/schema/nodes",
        "//kythe/proto:common_go_proto",
        "//kythe/proto:internal_go_proto",
        "//kythe/proto:serving_go_proto",
        "@com_github_golang_protobuf//proto:go_default_library",
    ],
)

go_test(
    name = "paths_test",
    size = "small",
    srcs = ["paths_test.go"],
    library = "paths",
    visibility = ["//visibility:private"],
    deps = ["//kythe/go/test/testutil"],
)
