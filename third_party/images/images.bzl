"""Pinned third-party container images."""

load("@rules_img//img:pull.bzl", "pull")

def _images_impl(_module_ctx):
    pull(
        name = "distroless_base_debian13_nonroot",
        digest = "sha256:97b9d04bed1c754b756c3c4b6a04915c22fb0b5d96a59944eb3bf78c26e6e157",
        registry = "gcr.io",
        repository = "distroless/base-debian13",
        tag = "nonroot",
    )

images = module_extension(implementation = _images_impl)
