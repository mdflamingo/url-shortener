// Package a - тестовый пакет для проверки анализатора кода.
package a

import (
	"log"
	"os"
)

// TestPanic проверяет обнаружение вызова panic.
func TestPanic() {
	panic("test") // want "avoid using panic"
}

// TestLogFatal проверяет обнаружение вызова log.Fatal вне main.
func TestLogFatal() {
	log.Fatal("test") // want "log.Fatal should only be used in main function"
}

// TestOsExit проверяет обнаружение вызова os.Exit вне main.
func TestOsExit() {
	os.Exit(1) // want "os.Exit should only be used in main function"
}
