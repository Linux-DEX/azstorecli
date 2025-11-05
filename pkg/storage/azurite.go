package storage

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

var currentContainerID string

// StartAzurite runs Azurite if not already running, using persistent storage with Docker volume.
func StartAzurite() (<-chan string, error) {
	logChan := make(chan string, 200)

	go func() {
		defer close(logChan)
		logChan <- "Starting Azurite...\n"

		id := GetAzuriteContainerID()
		if id == "" {
			// No container exists: create with named container & volume for persistence
			cmd := exec.Command("docker", "run", "-d",
				"--name", "azurite-emulator",
				"-p", "10000:10000",
				"-p", "10001:10001",
				"-p", "10002:10002",
				"-v", "azurite_data:/data", // persistent Docker volume
				"mcr.microsoft.com/azure-storage/azurite",
			)
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out
			if err := cmd.Run(); err != nil {
				logChan <- fmt.Sprintf("Error starting Azurite: %v\n", err)
				return
			}
			id = strings.TrimSpace(out.String())
			logChan <- fmt.Sprintf("Azurite container started: %s\n", id)
			time.Sleep(2 * time.Second)
		} else {
			// Container exists: start if stopped
			statusCmd := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", id)
			status, _ := statusCmd.Output()
			if strings.TrimSpace(string(status)) != "true" {
				startCmd := exec.Command("docker", "start", id)
				startCmd.Run()
				logChan <- fmt.Sprintf("Started existing Azurite container: %s\n", id)
				time.Sleep(2 * time.Second)
			}
			logChan <- fmt.Sprintf("Using existing Azurite container: %s\n", id)
		}

		currentContainerID = id
		streamDockerLogs(id, logChan)
	}()

	return logChan, nil
}

// AttachLogs attaches log streaming to an existing container by ID.
func AttachLogs(containerID string) (<-chan string, error) {
	logChan := make(chan string, 200)
	go func() {
		defer close(logChan)
		streamDockerLogs(containerID, logChan)
	}()
	return logChan, nil
}

func streamDockerLogs(containerID string, logChan chan<- string) {
	logChan <- "Attaching to Azurite logs...\n"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "logs", "-f", containerID)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logChan <- fmt.Sprintf("Error getting stdout pipe: %v\n", err)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		logChan <- fmt.Sprintf("Error getting stderr pipe: %v\n", err)
		return
	}
	if err := cmd.Start(); err != nil {
		logChan <- fmt.Sprintf("Error starting log stream: %v\n", err)
		return
	}

	go copyToChan(stdout, logChan)
	go copyToChan(stderr, logChan)
	cmd.Wait()
}

func copyToChan(r io.Reader, ch chan<- string) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		ch <- scanner.Text() + "\n"
	}
	if err := scanner.Err(); err != nil {
		ch <- fmt.Sprintf("Error reading logs: %v\n", err)
	}
}

// GetAzuriteContainerID returns the Docker container ID for 'azurite-emulator'.
func GetAzuriteContainerID() string {
	cmd := exec.Command("docker", "ps", "-aq", "--filter", "name=azurite-emulator")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// StopAzurite stops the running Azurite container but does not remove it.
func StopAzurite() {
	if currentContainerID == "" {
		return
	}
	exec.Command("docker", "stop", currentContainerID).Run()
	currentContainerID = ""
}


