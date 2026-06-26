// Copyright (c) 2026 CiteNet Internet. All rights reserved.
// Author: Michael Moscovitch
// SPDX-License-Identifier: MIT
// See the LICENSE file in the repository root for details.

package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

type Level int

const (
	LevelInfo Level = iota
	LevelDebug
	LevelVerbose
)

type Logger struct {
	level  Level
	out    io.Writer
	fields map[string]any
}

var std = &Logger{level: LevelInfo, out: os.Stdout}

func SetLevel(l Level) { std.level = l }
func GetLevel() Level  { return std.level }

func ParseLevel(s string) Level {
	switch s {
	case "debug":
		return LevelDebug
	case "verbose":
		return LevelVerbose
	default:
		return LevelInfo
	}
}

func With(fields map[string]any) *Logger {
	merged := make(map[string]any, len(fields))
	for k, v := range std.fields {
		merged[k] = v
	}
	for k, v := range fields {
		merged[k] = v
	}
	return &Logger{level: std.level, out: std.out, fields: merged}
}

func (l *Logger) With(fields map[string]any) *Logger {
	merged := make(map[string]any, len(l.fields)+len(fields))
	for k, v := range l.fields {
		merged[k] = v
	}
	for k, v := range fields {
		merged[k] = v
	}
	return &Logger{level: l.level, out: l.out, fields: merged}
}

func (l *Logger) log(lvl, event string, fields map[string]any) {
	entry := map[string]any{
		"ts":    time.Now().UTC().Format(time.RFC3339),
		"level": lvl,
		"event": event,
	}
	for k, v := range l.fields {
		entry[k] = v
	}
	for k, v := range fields {
		entry[k] = v
	}
	b, err := json.Marshal(entry)
	if err != nil {
		fmt.Fprintf(l.out, `{"ts":%q,"level":"error","event":"log_marshal_error","error":%q}`+"\n",
			time.Now().UTC().Format(time.RFC3339), err.Error())
		return
	}
	fmt.Fprintln(l.out, string(b))
}

func (l *Logger) Info(event string, fields ...map[string]any)    { l.log("info", event, merge(fields)) }
func (l *Logger) Debug(event string, fields ...map[string]any) {
	if l.level >= LevelDebug {
		l.log("debug", event, merge(fields))
	}
}
func (l *Logger) Verbose(event string, fields ...map[string]any) {
	if l.level >= LevelVerbose {
		l.log("verbose", event, merge(fields))
	}
}
func (l *Logger) Warn(event string, fields ...map[string]any)  { l.log("warn", event, merge(fields)) }
func (l *Logger) Error(event string, fields ...map[string]any) { l.log("error", event, merge(fields)) }

func Info(event string, fields ...map[string]any)    { std.Info(event, fields...) }
func Debug(event string, fields ...map[string]any)   { std.Debug(event, fields...) }
func Verbose(event string, fields ...map[string]any) { std.Verbose(event, fields...) }
func Warn(event string, fields ...map[string]any)    { std.Warn(event, fields...) }
func Error(event string, fields ...map[string]any)   { std.Error(event, fields...) }

func merge(ms []map[string]any) map[string]any {
	if len(ms) == 0 {
		return nil
	}
	out := make(map[string]any)
	for _, m := range ms {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}
