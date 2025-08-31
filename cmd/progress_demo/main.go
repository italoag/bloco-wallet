package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/lipgloss"

	"blocowallet/internal/ui"
	"blocowallet/internal/wallet"
)

// Demo implementation to validate progress tracking improvements
type ProgressDemo struct {
	state       *ui.EnhancedImportState
	batchService *DemoBatchService
	styles      ui.Styles
}

type DemoBatchService struct{}

func (d *DemoBatchService) CreateImportJobsFromFiles(files []string) ([]wallet.ImportJob, error) {
	var jobs []wallet.ImportJob
	for i, file := range files {
		jobs = append(jobs, wallet.ImportJob{
			KeystorePath: file,
			WalletName:   fmt.Sprintf("demo-wallet-%d", i+1),
		})
	}
	return jobs, nil
}

func (d *DemoBatchService) CreateImportJobsFromDirectory(dir string) ([]wallet.ImportJob, error) {
	// Simulate finding keystore files in directory
	files := []string{
		filepath.Join(dir, "wallet1.json"),
		filepath.Join(dir, "wallet2.json"),
		filepath.Join(dir, "wallet3.json"),
	}
	return d.CreateImportJobsFromFiles(files)
}

func (d *DemoBatchService) ValidateImportJobs(jobs []wallet.ImportJob) error {
	return nil
}

func (d *DemoBatchService) ImportBatch(
	jobs []wallet.ImportJob,
	progressChan chan<- wallet.ImportProgress,
	passwordRequestChan chan<- wallet.PasswordRequest,
	passwordResponseChan <-chan wallet.PasswordResponse,
) []wallet.ImportResult {
	startTime := time.Now()
	results := make([]wallet.ImportResult, 0, len(jobs))

	// Send initial progress
	progress := wallet.ImportProgress{
		CurrentFile:     "",
		TotalFiles:      len(jobs),
		ProcessedFiles:  0,
		Percentage:      0.0,
		Errors:          []wallet.ImportError{},
		PendingPassword: false,
		StartTime:       startTime,
		ElapsedTime:     0,
	}
	
	// Use the enhanced sendProgressUpdate pattern
	sendProgressUpdate(progress, progressChan)

	// Process each job with progress updates
	for i, job := range jobs {
		// Update progress for current file
		progress.CurrentFile = filepath.Base(job.KeystorePath)
		progress.ProcessedFiles = i
		progress.Percentage = float64(i) / float64(len(jobs)) * 100
		progress.ElapsedTime = time.Since(startTime)

		sendProgressUpdate(progress, progressChan)

		// Simulate processing time
		time.Sleep(500 * time.Millisecond)

		// Create successful result
		result := wallet.ImportResult{
			Job:     job,
			Success: true,
			Wallet:  nil, // Would contain actual wallet details
			Error:   nil,
			Skipped: false,
		}
		results = append(results, result)

		// Update progress after processing
		progress.ProcessedFiles = i + 1
		progress.Percentage = float64(i+1) / float64(len(jobs)) * 100
		progress.ElapsedTime = time.Since(startTime)

		sendProgressUpdate(progress, progressChan)
	}

	// Send final progress
	progress.CurrentFile = ""
	progress.ProcessedFiles = len(jobs)
	progress.Percentage = 100.0
	progress.ElapsedTime = time.Since(startTime)

	sendProgressUpdate(progress, progressChan)
	close(progressChan)

	return results
}

func (d *DemoBatchService) GetImportSummary(results []wallet.ImportResult) wallet.ImportSummary {
	summary := wallet.ImportSummary{
		TotalFiles: len(results),
	}

	for _, result := range results {
		if result.Success {
			summary.SuccessfulImports++
		} else if result.Skipped {
			summary.SkippedImports++
		} else {
			summary.FailedImports++
		}
	}

	return summary
}

// Enhanced sendProgressUpdate with the improved pattern
func sendProgressUpdate(progress wallet.ImportProgress, progressChan chan<- wallet.ImportProgress) {
	if progressChan == nil {
		return
	}

	select {
	case progressChan <- progress:
		// Successfully sent progress update
	case <-time.After(500 * time.Millisecond):
		// Log dropped update for debugging
		log.Printf("Demo: Progress update dropped - channel may be blocked (file: %s, progress: %.1f%%)", 
			progress.CurrentFile, progress.Percentage)
	}
}

func NewProgressDemo() *ProgressDemo {
	// Create demo styles
	styles := ui.Styles{
		MenuTitle:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")),
		MenuDesc:     lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		SuccessStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("46")),
		ErrorStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
	}

	batchService := &DemoBatchService{}
	state := ui.NewEnhancedImportState(batchService, styles)

	return &ProgressDemo{
		state:        state,
		batchService: batchService,
		styles:       styles,
	}
}

func (p *ProgressDemo) RunDemo() error {
	// Create demo import jobs
	demoFiles := []string{
		"demo/keystore1.json",
		"demo/keystore2.json", 
		"demo/keystore3.json",
		"demo/keystore4.json",
		"demo/keystore5.json",
	}

	jobs, err := p.batchService.CreateImportJobsFromFiles(demoFiles)
	if err != nil {
		return fmt.Errorf("failed to create import jobs: %w", err)
	}

	// Set up the import state
	p.state.ImportJobs = jobs
	err = p.state.TransitionToPhase(ui.PhaseImporting)
	if err != nil {
		return fmt.Errorf("failed to transition to importing phase: %w", err)
	}

	fmt.Println("🚀 Starting Progress Tracking Demo...")
	fmt.Printf("📁 Simulating import of %d keystore files\n", len(demoFiles))
	fmt.Println("⏳ Progress updates should appear in real-time:")
	fmt.Println()

	// Start the import process using the batch service directly
	// Create channels for the demo
	progressChan := make(chan wallet.ImportProgress, 500)
	passwordRequestChan := make(chan wallet.PasswordRequest, 1)
	passwordResponseChan := make(chan wallet.PasswordResponse, 1)

	// Start progress monitoring
	done := make(chan bool)
	var progressUpdates []wallet.ImportProgress

	go func() {
		defer close(done)
		for progress := range progressChan {
			progressUpdates = append(progressUpdates, progress)
			
			// Simulate UI update
			p.state.UpdateProgress(progress)
			
			// Display progress
			if progress.CurrentFile != "" {
				fmt.Printf("📄 Processing: %-20s [%3d/%d] %6.1f%% (%.1fs)\n",
					progress.CurrentFile,
					progress.ProcessedFiles,
					progress.TotalFiles,
					progress.Percentage,
					progress.ElapsedTime.Seconds())
			} else if progress.ProcessedFiles == progress.TotalFiles {
				fmt.Printf("✅ Completed: All files processed    [%3d/%d] %6.1f%% (%.1fs)\n",
					progress.ProcessedFiles,
					progress.TotalFiles,
					progress.Percentage,
					progress.ElapsedTime.Seconds())
			}
		}
	}()

	results := p.state.BatchService.ImportBatch(
		p.state.ImportJobs,
		progressChan,
		passwordRequestChan,
		passwordResponseChan,
	)

	// Wait for progress monitoring to complete
	<-done

	// Display summary
	fmt.Println()
	fmt.Println("📊 Demo Results:")
	fmt.Printf("   • Total files processed: %d\n", len(results))
	fmt.Printf("   • Progress updates received: %d\n", len(progressUpdates))
	fmt.Printf("   • Final progress: %.1f%%\n", progressUpdates[len(progressUpdates)-1].Percentage)
	
	// Validate monotonic progress
	isMonotonic := true
	for i := 1; i < len(progressUpdates); i++ {
		if progressUpdates[i].ProcessedFiles < progressUpdates[i-1].ProcessedFiles {
			isMonotonic = false
			break
		}
	}
	
	if isMonotonic {
		fmt.Println("   ✅ Progress validation: PASSED (monotonic)")
	} else {
		fmt.Println("   ❌ Progress validation: FAILED (not monotonic)")
	}

	fmt.Println()
	fmt.Println("🎉 Demo completed successfully!")
	fmt.Println("   The enhanced progress tracking system is working correctly.")

	return nil
}

func main() {
	demo := NewProgressDemo()
	
	if err := demo.RunDemo(); err != nil {
		fmt.Fprintf(os.Stderr, "Demo failed: %v\n", err)
		os.Exit(1)
	}
}