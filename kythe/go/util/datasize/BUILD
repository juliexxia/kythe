load("//tools:build_rules/shims.bzl", "go_test", "go_library")

package(default_visibility = ["//kythe:default_visibility"])

go_library(
    name = "datasize",
    srcs = ["datasize.go"],
)

go_test(
    name = "datasize_test",
    size = "small",
    srcs = ["datasize_test.go"],
    library = "datasize",
    visibility = ["//visibility:private"],
    deps = ["//kythe/go/test/testutil"],
)
