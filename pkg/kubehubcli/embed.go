package kubehubcli

import "embed"

//go:embed configassets/*
var NodeConfigs embed.FS

//go:embed podassets/*
var StaticPods embed.FS
