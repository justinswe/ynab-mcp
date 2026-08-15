"""Rules for packaging Go binaries as container images."""

load("@rules_img//img:image.bzl", "image_from_binary")
load("@rules_img//img:layer.bzl", "image_layer", "layer_from_binary")
load("@rules_img//img:load.bzl", "image_load")
load("@rules_img//img:push.bzl", "image_push")

def go_container_image(
        name,
        binary,
        base,
        platforms,
        registry,
        repository,
        tag = "latest",
        additional_binaries = {},
        additional_tags = {},
        files = {},
        file_metadata = {},
        visibility = None,
        **kwargs):
    """Creates an image plus runnable load and push targets for a Go binary.

    The generated targets are `<name>`, `<name>_load`, and `<name>_push`.

    Args:
        name: Name of the OCI image target.
        binary: Go binary target to package.
        base: Base image target.
        platforms: Target platforms for the image; more than one produces an image index.
        registry: Destination registry for the push target.
        repository: Destination repository and local image name.
        tag: Image tag for load and push operations.
        additional_binaries: Mapping of absolute container paths to additional binary labels.
        additional_tags: Mapping of target suffixes to additional image tags.
        files: Mapping of absolute container paths to source labels.
        file_metadata: Mapping of absolute container paths in `files` to
            `file_metadata` values, used to make packaged files executable.
        visibility: Visibility applied to the generated public targets.
        **kwargs: Additional attributes forwarded to `image_from_binary`.
    """
    layers = []
    for index, path in enumerate(sorted(additional_binaries.keys())):
        layer_name = name + "_binary_" + str(index)
        layer_from_binary(
            name = layer_name,
            binary = additional_binaries[path],
            include_runfiles = False,
            path = path,
            visibility = ["//visibility:private"],
        )
        layers.append(":" + layer_name)

    if files:
        layer_name = name + "_files"
        image_layer(
            name = layer_name,
            srcs = files,
            file_metadata = file_metadata,
            visibility = ["//visibility:private"],
        )
        layers.append(":" + layer_name)

    image_from_binary(
        name = name,
        base = base,
        binary = binary,
        include_runfiles = False,
        layers = layers,
        path = "/app/" + _target_name(binary),
        platforms = platforms,
        visibility = visibility,
        working_dir = "/app",
        **kwargs
    )

    image_load(
        name = name + "_load",
        image = ":" + name,
        tag = repository + ":" + tag,
        tags = ["manual"],
        visibility = visibility,
    )

    image_push(
        name = name + "_push",
        image = ":" + name,
        registry = registry,
        repository = repository,
        tag = tag,
        tags = ["manual"],
        visibility = visibility,
    )

    for suffix, additional_tag in additional_tags.items():
        image_push(
            name = name + "_push_" + suffix,
            image = ":" + name,
            registry = registry,
            repository = repository,
            tag = additional_tag,
            tags = ["manual"],
            visibility = visibility,
        )

def _target_name(label):
    if ":" in label:
        return label.split(":")[-1]
    return label.split("/")[-1]
