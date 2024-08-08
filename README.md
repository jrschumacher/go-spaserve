# Go SPA Serve 
[![codecov](https://codecov.io/gh/jrschumacher/go-spaserve/graph/badge.svg?token=W99WAK10IX)](https://codecov.io/gh/jrschumacher/go-spaserve)

Go SPA Serve is a simple package that serves static files specifically focused on serving single page applications (SPA).
Generally, this can be used to instead of the stdlib `http.FileServer`, but the main use case is to serve a SPA with a
single entry point (e.g. `index.html`) and let the client-side router handle the rest.

In addition to serving static files, this package also provides a way to load environment variables from a Go struct at
runtime by injecting them into the head tag of the served HTML file.

## Problem

Vite is a fantastic build tool, but it doesn't support loading environment variables at runtime.
This becomes quite a problem when you build a single image for multiple environments or need to
build images for on-premises deployments where you can't bake in the environment variables.
