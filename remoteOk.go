package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/go-api-libs/remote-ok-jobs/pkg/remoteokjobs"
)

func main() {
	// Логирование в консоль
	log.SetFlags(log.LstdFlags)
	log.Println("Создаем клиент RemoteOK...")

	client, err := remoteokjobs.NewClient()
	if err != nil {
		log.Fatalf("Ошибка при создании клиента: %v", err)
	}

	log.Println("Загружаем вакансии с RemoteOK...")
	jobs, err := client.GetJobs(context.Background())
	if err != nil {
		log.Fatalf("Ошибка при получении вакансий: %v", err)
	}

	log.Printf("Всего вакансий получено: %d", len(jobs))

	// Сортировка всех вакансий с ненулевой зарплатой по максимальной зарплате (убывание)
	var validJobs []remoteokjobs.Job
	for _, job := range jobs {
		if job.SalaryMin != nil && job.SalaryMax != nil && *job.SalaryMax > 0 {
			validJobs = append(validJobs, job)
		}
	}
	sort.Slice(validJobs, func(i, j int) bool { return *validJobs[i].SalaryMax > *validJobs[j].SalaryMax })

	// Создаём файл для записи результатов
	file, err := os.Create("results.txt")
	if err != nil {
		log.Fatalf("Ошибка при создании файла: %v", err)
	}
	defer file.Close()

	// old writeJob closure removed; using writeJobWithMonthYear helper

	// Порог в рублях (по требованию) — 120000 руб/месяц
	const highRubThreshold = 120000.0
	// Низкий порог в рублях (используем разумное значение, можно изменить)
	const lowRubThreshold = 15000.0

	// Жёстко задаём курс RUB per 1 USD
	rate := 80.0

	// Конвертируем рублёвые пороги в USD/месяц (для удобства сравнения)
	highUsdThreshold := highRubThreshold / rate
	lowUsdThreshold := lowRubThreshold / rate

	var highSalaryJobs, lowSalaryJobs, midSalaryJobs []remoteokjobs.Job
	var missingSalaryCount int

	// Сбор статистики по тэгам: суммарная месячная зарплата (RUB) и количество вакансий
	tagTotals := make(map[string]float64)
	tagCounts := make(map[string]int)

	// Также вычислим максимальную длину названия вакансии для выравнивания таблицы
	maxPosLen := 20

	for _, job := range validJobs {
		if job.Position != nil {
			// используем количество рун для корректного учёта кириллицы
			if l := len([]rune(*job.Position)); l > maxPosLen {
				maxPosLen = l
			}
		}

		if job.SalaryMin == nil || job.SalaryMax == nil || *job.SalaryMax <= 0 {
			missingSalaryCount++
			continue
		}
		// monthly salary in USD (assume provided salaries are yearly USD)
		monthlyUsd := float64(*job.SalaryMax) / 12.0
		monthlyRub := monthlyUsd * rate

		if monthlyRub >= highRubThreshold {
			highSalaryJobs = append(highSalaryJobs, job)
		} else if monthlyRub < lowRubThreshold {
			lowSalaryJobs = append(lowSalaryJobs, job)
		} else {
			midSalaryJobs = append(midSalaryJobs, job)
		}

		// Собираем тэги — проверяем на nil и на пустой срез
		if job.Tags != nil {
			for _, t := range job.Tags {
				if t == "" {
					continue
				}
				tagTotals[t] += monthlyRub
				tagCounts[t]++
			}
		}
	}

	// Вывод: высокие вакансии (без ссылок), показываем месячную и годовую зарплату
	fmt.Printf("=== ВАКАНСИИ С ВЫСОКОЙ ЗАРПЛАТОЙ (>= %.0f RUB/мес -> >= $%.2f/мес) ===\n", highRubThreshold, highUsdThreshold)
	file.WriteString(fmt.Sprintf("=== ВАКАНСИИ С ВЫСОКОЙ ЗАРПЛАТОЙ (>= %.0f RUB/мес -> >= $%.2f/мес) ===\n", highRubThreshold, highUsdThreshold))
	for _, job := range highSalaryJobs {
		line := writeJobWithMonthYear(job, rate, highRubThreshold, maxPosLen)
		fmt.Println("- " + line)
		file.WriteString("- " + line + "\n")
	}

	// Вывод: низкие вакансии
	fmt.Printf("\n=== ВАКАНСИИ С НИЗКОЙ ЗАРПЛАТОЙ (< %.0f RUB/мес -> < $%.2f/мес) ===\n", lowRubThreshold, lowUsdThreshold)
	file.WriteString(fmt.Sprintf("\n=== ВАКАНСИИ С НИЗКОЙ ЗАРПЛАТОЙ (< %.0f RUB/мес -> < $%.2f/мес) ===\n", lowRubThreshold, lowUsdThreshold))
	for _, job := range lowSalaryJobs {
		line := writeJobWithMonthYear(job, rate, highRubThreshold, maxPosLen)
		fmt.Println("- " + line)
		file.WriteString("- " + line + "\n")
	}

	// Вакансии между порогами
	fmt.Printf("\n=== ВАКАНСИИ МЕЖДУ %v и %v RUB/мес ===\n", lowRubThreshold, highRubThreshold)
	file.WriteString(fmt.Sprintf("\n=== ВАКАНСИИ МЕЖДУ %v и %v RUB/мес ===\n", lowRubThreshold, highRubThreshold))
	for _, job := range midSalaryJobs {
		line := writeJobWithMonthYear(job, rate, highRubThreshold, maxPosLen)
		fmt.Println("- " + line)
		file.WriteString("- " + line + "\n")
	}

	// Выводим статистику по отсутствующим/между/неподходящим
	fmt.Println()
	file.WriteString("\n")
	fmt.Printf("Вакансий без указанной зарплаты: %d\n", missingSalaryCount)
	file.WriteString(fmt.Sprintf("Вакансий без указанной зарплаты: %d\n", missingSalaryCount))

	fmt.Printf("Вакансий между порогами (не подходят под ограничение): %d\n", len(midSalaryJobs))
	file.WriteString(fmt.Sprintf("Вакансий между порогами (не подходят под ограничение): %d\n", len(midSalaryJobs)))

	// --- Сбор и вывод таблицы по навыкам (тэгам) ---
	type tagStat struct {
		Tag    string
		AvgRub float64
		AvgUsd float64
		Count  int
	}
	var tagsStats []tagStat
	for t, total := range tagTotals {
		cnt := tagCounts[t]
		if cnt <= 0 {
			continue
		}
		avg := total / float64(cnt)
		tagsStats = append(tagsStats, tagStat{Tag: t, AvgRub: avg, AvgUsd: avg / rate, Count: cnt})
	}
	// Сортируем по средней зарплате по убыванию
	sort.Slice(tagsStats, func(i, j int) bool { return tagsStats[i].AvgRub > tagsStats[j].AvgRub })

	// Определяем ширину колонки для тегов
	tagColWidth := 10
	for _, ts := range tagsStats {
		if l := len([]rune(ts.Tag)); l > tagColWidth {
			tagColWidth = l
		}
	}

	fmt.Printf("\n=== НАВЫКИ (тэги) — СРЕДНЯЯ ЗП ПО ТЭГАМ (по вакансиям), сортировка по убыванию ===\n")
	file.WriteString("\n=== НАВЫКИ (тэги) — СРЕДНЯЯ ЗП ПО ТЭГАМ (по вакансиям), сортировка по убыванию ===\n")
	for _, ts := range tagsStats {
		line := fmt.Sprintf("%-*s %.0f RUB/mo ($%.2f/mo) (vacancies: %d)", tagColWidth, ts.Tag, ts.AvgRub, ts.AvgUsd, ts.Count)
		fmt.Println("- " + line)
		file.WriteString("- " + line + "\n")
	}

	log.Println("Готово. Результаты записаны в results.txt")
}

// API-based rate fetching removed — используем захардкоженный курс

func writeJobWithMonthYear(job remoteokjobs.Job, rate float64, highRubThreshold float64, width int) string {
	sMaxYear := *job.SalaryMax
	monthlyUsd := float64(sMaxYear) / 12.0
	monthlyRub := monthlyUsd * rate
	pos := "(no position)"
	if job.Position != nil {
		pos = *job.Position
	}
	// знак по отношению к highRubThreshold
	sign := "<"
	if monthlyRub >= highRubThreshold {
		sign = ">"
	}
	// Формат: Position — >150000 RUB/mo ($1875.00/mo) с динамической шириной колонки
	return fmt.Sprintf("%-*s %s%.0f RUB/mo ($%.2f/mo)", width, pos, sign, monthlyRub, monthlyUsd)
}

// processWithUsdThresholds — fallback на старую логику, если не удалось получить курс
// processWithUsdThresholds removed — больше не используем API/fallback
