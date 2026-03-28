package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"

	"github.com/go-api-libs/remote-ok-jobs/pkg/remoteokjobs"
)

// Config содержит конфигурацию приложения
type Config struct {
	HighRubThreshold float64
	LowRubThreshold  float64
	RubPerUSD        float64
}

// tagStat представляет статистику по навыку
type tagStat struct {
	Tag    string
	AvgRub float64
	AvgUsd float64
	Count  int
}

// jobTableRow представляет строку в таблице вакансий
type jobTableRow struct {
	Position   string
	MonthlyRub float64
	MonthlyUsd float64
	YearlyRub  float64
	YearlyUsd  float64
}

// initConfig инициализирует конфигурацию из флагов командной строки или значений по умолчанию
func initConfig() *Config {
	config := &Config{
		HighRubThreshold: 120000.0,
		LowRubThreshold:  15000.0,
		RubPerUSD:        80.0,
	}

	flag.Float64Var(&config.HighRubThreshold, "high-threshold", config.HighRubThreshold, "Порог высокой зарплаты в RUB/месяц")
	flag.Float64Var(&config.LowRubThreshold, "low-threshold", config.LowRubThreshold, "Порог низкой зарплаты в RUB/месяц")
	flag.Float64Var(&config.RubPerUSD, "rate", config.RubPerUSD, "Курс RUB за 1 USD")
	flag.Parse()

	return config
}

// fetchJobs получает вакансии из API RemoteOK
func fetchJobs(ctx context.Context) ([]remoteokjobs.Job, error) {
	client, err := remoteokjobs.NewClient()
	if err != nil {
		return nil, fmt.Errorf("не удалось создать клиент RemoteOK: %w", err)
	}

	jobs, err := client.GetJobs(ctx)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить вакансии: %w", err)
	}

	return jobs, nil
}

// filterValidJobs фильтрует вакансии с корректной информацией о зарплате и сортирует по зарплате по убыванию
func filterValidJobs(jobs []remoteokjobs.Job) []remoteokjobs.Job {
	var validJobs []remoteokjobs.Job
	for _, job := range jobs {
		if job.SalaryMin != nil && job.SalaryMax != nil && *job.SalaryMax > 0 {
			validJobs = append(validJobs, job)
		}
	}
	sort.Slice(validJobs, func(i, j int) bool {
		return *validJobs[i].SalaryMax > *validJobs[j].SalaryMax
	})
	return validJobs
}

// categorizeJobs категоризирует вакансии на высоко- и низкооплачиваемые на основе порогов
func categorizeJobs(jobs []remoteokjobs.Job, config *Config) (highSalaryJobs, lowSalaryJobs []remoteokjobs.Job) {
	for _, job := range jobs {
		monthlyUsd := float64(*job.SalaryMax) / 12.0
		monthlyRub := monthlyUsd * config.RubPerUSD

		if monthlyRub >= config.HighRubThreshold {
			highSalaryJobs = append(highSalaryJobs, job)
		} else if monthlyRub < config.LowRubThreshold {
			lowSalaryJobs = append(lowSalaryJobs, job)
		}
	}
	return
}

// collectTagStats собирает статистику по навыкам
func collectTagStats(jobs []remoteokjobs.Job, config *Config) []tagStat {
	tagTotals := make(map[string]float64)
	tagCounts := make(map[string]int)

	for _, job := range jobs {
		monthlyUsd := float64(*job.SalaryMax) / 12.0
		monthlyRub := monthlyUsd * config.RubPerUSD

		if job.Tags != nil {
			for _, tag := range job.Tags {
				if tag == "" {
					continue
				}
				tagTotals[tag] += monthlyRub
				tagCounts[tag]++
			}
		}
	}

	var tagsStats []tagStat
	for tag, total := range tagTotals {
		count := tagCounts[tag]
		if count <= 0 {
			continue
		}
		avgRub := total / float64(count)
		tagsStats = append(tagsStats, tagStat{
			Tag:    tag,
			AvgRub: avgRub,
			AvgUsd: avgRub / config.RubPerUSD,
			Count:  count,
		})
	}

	// Сортировка по средней зарплате по убыванию
	sort.Slice(tagsStats, func(i, j int) bool {
		return tagsStats[i].AvgRub > tagsStats[j].AvgRub
	})

	return tagsStats
}

// createCSVWriters создает CSV writers для выходных файлов
func createCSVWriters() (*csv.Writer, *csv.Writer, *csv.Writer, func(), error) {
	highFile, err := os.Create("high_salary.csv")
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("не удалось создать high_salary.csv: %w", err)
	}

	lowFile, err := os.Create("low_salary.csv")
	if err != nil {
		highFile.Close()
		return nil, nil, nil, nil, fmt.Errorf("не удалось создать low_salary.csv: %w", err)
	}

	skillsFile, err := os.Create("skills_avg_salary.csv")
	if err != nil {
		highFile.Close()
		lowFile.Close()
		return nil, nil, nil, nil, fmt.Errorf("не удалось создать skills_avg_salary.csv: %w", err)
	}

	highWriter := csv.NewWriter(highFile)
	lowWriter := csv.NewWriter(lowFile)
	skillsWriter := csv.NewWriter(skillsFile)

	cleanup := func() {
		highWriter.Flush()
		lowWriter.Flush()
		skillsWriter.Flush()
		highFile.Close()
		lowFile.Close()
		skillsFile.Close()
	}

	return highWriter, lowWriter, skillsWriter, cleanup, nil
}

// generateReports генерирует и выводит все отчеты
func generateReports(highJobs, lowJobs []remoteokjobs.Job, tagsStats []tagStat, config *Config, highWriter, lowWriter, skillsWriter *csv.Writer) {
	printJobTable("=== ВАКАНСИИ С ВЫСОКОЙ ЗАРПЛАТОЙ (>= "+fmt.Sprintf("%.0f", config.HighRubThreshold)+" RUB/мес) ===", highJobs, config, highWriter)
	fmt.Println()
	printJobTable("=== ВАКАНСИИ С НИЗКОЙ ЗАРПЛАТОЙ (< "+fmt.Sprintf("%.0f", config.LowRubThreshold)+" RUB/мес) ===", lowJobs, config, lowWriter)
	fmt.Println()
	printTagTables(tagsStats, config, skillsWriter)
}

func main() {
	log.SetFlags(log.LstdFlags)

	config := initConfig()

	log.Println("Получение вакансий из RemoteOK...")
	jobs, err := fetchJobs(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Всего получено вакансий: %d", len(jobs))

	missingSalaryCount := 0
	for _, job := range jobs {
		if job.SalaryMin == nil || job.SalaryMax == nil || *job.SalaryMax <= 0 {
			missingSalaryCount++
		}
	}
	log.Printf("Вакансий без информации о зарплате: %d", missingSalaryCount)

	validJobs := filterValidJobs(jobs)
	highSalaryJobs, lowSalaryJobs := categorizeJobs(validJobs, config)
	tagsStats := collectTagStats(validJobs, config)

	highWriter, lowWriter, skillsWriter, cleanup, err := createCSVWriters()
	if err != nil {
		log.Fatal(err)
	}
	defer cleanup()

	generateReports(highSalaryJobs, lowSalaryJobs, tagsStats, config, highWriter, lowWriter, skillsWriter)

	log.Println("Готово. Результаты записаны в CSV файлы: high_salary.csv, low_salary.csv, skills_avg_salary.csv")
}

// buildJobTableRows строит строки таблицы из вакансий
func buildJobTableRows(jobs []remoteokjobs.Job, config *Config) []jobTableRow {
	rows := make([]jobTableRow, 0, len(jobs))
	for _, job := range jobs {
		if job.SalaryMax == nil || *job.SalaryMax <= 0 {
			continue
		}
		monthlyUsd := float64(*job.SalaryMax) / 12.0
		monthlyRub := monthlyUsd * config.RubPerUSD
		yearlyUsd := float64(*job.SalaryMax)
		yearlyRub := yearlyUsd * config.RubPerUSD
		pos := "(no position)"
		if job.Position != nil {
			pos = *job.Position
		}
		rows = append(rows, jobTableRow{
			Position:   pos,
			MonthlyRub: monthlyRub,
			MonthlyUsd: monthlyUsd,
			YearlyRub:  yearlyRub,
			YearlyUsd:  yearlyUsd,
		})
	}
	return rows
}

// printJobTable выводит таблицу вакансий в консоль и CSV
func printJobTable(title string, jobs []remoteokjobs.Job, config *Config, csvWriter *csv.Writer) {
	rows := buildJobTableRows(jobs, config)
	if len(rows) == 0 {
		fmt.Println(title)
		fmt.Println("(нет данных)")
		return
	}

	posWidth := len("Вакансия")
	rubW := len("RUB/мес")
	usdW := len("USD/мес")
	yearRubW := len("RUB/год")
	yearUsdW := len("USD/год")

	for _, r := range rows {
		if l := len([]rune(r.Position)); l > posWidth {
			posWidth = l
		}
		rubStr := fmt.Sprintf("%.0f", r.MonthlyRub)
		if l := len(rubStr); l > rubW {
			rubW = l
		}
		usdStr := fmt.Sprintf("%.2f", r.MonthlyUsd)
		if l := len(usdStr); l > usdW {
			usdW = l
		}
		yearRubStr := fmt.Sprintf("%.0f", r.YearlyRub)
		if l := len(yearRubStr); l > yearRubW {
			yearRubW = l
		}
		yearUsdStr := fmt.Sprintf("%.2f", r.YearlyUsd)
		if l := len(yearUsdStr); l > yearUsdW {
			yearUsdW = l
		}
	}

	header := fmt.Sprintf("%*s  %*s  %*s  %*s  %*s",
		-posWidth, "Вакансия",
		rubW, "RUB/мес",
		usdW, "USD/мес",
		yearRubW, "RUB/год",
		yearUsdW, "USD/год")
	fmt.Println(title)
	fmt.Println(header)

	// Write CSV header
	csvWriter.Write([]string{"Вакансия", "RUB/мес", "USD/мес", "RUB/год", "USD/год"})

	for _, r := range rows {
		line := fmt.Sprintf("%-*s  %*s  %*s  %*s  %*s",
			posWidth, r.Position,
			rubW, fmt.Sprintf("%.0f", r.MonthlyRub),
			usdW, fmt.Sprintf("%.2f", r.MonthlyUsd),
			yearRubW, fmt.Sprintf("%.0f", r.YearlyRub),
			yearUsdW, fmt.Sprintf("%.2f", r.YearlyUsd))
		fmt.Println(line)
		// Write CSV row
		csvWriter.Write([]string{
			r.Position,
			fmt.Sprintf("%.0f", r.MonthlyRub),
			fmt.Sprintf("%.2f", r.MonthlyUsd),
			fmt.Sprintf("%.0f", r.YearlyRub),
			fmt.Sprintf("%.2f", r.YearlyUsd),
		})
	}
}

// printTagTables выводит таблицы статистики по навыкам
func printTagTables(tagsStats []tagStat, config *Config, csvWriter *csv.Writer) {
	if len(tagsStats) == 0 {
		return
	}
	// Популярность по количеству — только в консоль
	tagPop := make([]tagStat, len(tagsStats))
	copy(tagPop, tagsStats)
	sort.Slice(tagPop, func(i, j int) bool {
		return tagPop[i].Count > tagPop[j].Count
	})

	// Расчет ширины колонок
	tagWidth := len("Навык")
	avgRubWidth := len("Средний RUB/мес")
	avgUsdWidth := len("Средний USD/мес")
	countWidth := len("Вакансий")
	for _, ts := range tagsStats {
		if l := len([]rune(ts.Tag)); l > tagWidth {
			tagWidth = l
		}
		if l := len(fmt.Sprintf("%.0f", ts.AvgRub)); l > avgRubWidth {
			avgRubWidth = l
		}
		if l := len(fmt.Sprintf("%.2f", ts.AvgUsd)); l > avgUsdWidth {
			avgUsdWidth = l
		}
		if l := len(fmt.Sprintf("%d", ts.Count)); l > countWidth {
			countWidth = l
		}
	}

	fmt.Println("\n=== НАВЫКИ (Средняя зарплата) ===")
	header1 := fmt.Sprintf("%-*s  %*s  %*s  %*s", tagWidth, "Навык", avgRubWidth, "Средний RUB/мес", avgUsdWidth, "Средний USD/мес", countWidth, "Вакансий")
	fmt.Println(header1)
	// Write CSV header for skills avg
	csvWriter.Write([]string{"Навык", "Средний RUB/мес", "Средний USD/мес", "Вакансий"})
	for _, ts := range tagsStats {
		line := fmt.Sprintf("%-*s  %*s  %*s  %*d", tagWidth, ts.Tag, avgRubWidth, fmt.Sprintf("%.0f", ts.AvgRub), avgUsdWidth, fmt.Sprintf("%.2f", ts.AvgUsd), countWidth, ts.Count)
		fmt.Println(line)
		// Write CSV row
		csvWriter.Write([]string{
			ts.Tag,
			fmt.Sprintf("%.0f", ts.AvgRub),
			fmt.Sprintf("%.2f", ts.AvgUsd),
			strconv.Itoa(ts.Count),
		})
	}

	fmt.Println("\n=== НАВЫКИ (Популярность по количеству вакансий) ===")
	header2 := header1
	fmt.Println(header2)
	for _, ts := range tagPop {
		line := fmt.Sprintf("%-*s  %*s  %*s  %*d", tagWidth, ts.Tag, avgRubWidth, fmt.Sprintf("%.0f", ts.AvgRub), avgUsdWidth, fmt.Sprintf("%.2f", ts.AvgUsd), countWidth, ts.Count)
		fmt.Println(line)
	}
}
