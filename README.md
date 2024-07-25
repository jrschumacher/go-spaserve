# Go Vite Env

Vite is a fantastic build tool, but it doesn't support loading environment variables at runtime.
This becomes quite a problem when you build a single image for multiple environments or need to
build images for on-premises deployments where you can't bake in the environment variables.

This package provides a way to load environment variables from a Go struct at runtime.
