package tasks

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/FelixSeptem/danny/internal/storage"
)

var (
	ErrInvalidCron        = errors.New("tasks: invalid cron expression")
	ErrInvalidTimezone    = errors.New("tasks: invalid timezone")
	ErrInvalidMisfire     = errors.New("tasks: invalid misfire policy")
	ErrOccurrenceNotFound = errors.New("tasks: no matching occurrence")
)

// OccurrenceCalculator resolves five-field cron expressions against IANA
// timezone data. Civil minutes are iterated in UTC so DST gaps and folds are
// handled by resolveCivilMinute rather than by time.Time's implicit choice.
type OccurrenceCalculator struct {
	TZDataVersion string
}

func NewOccurrenceCalculator(tzdataVersion string) OccurrenceCalculator {
	if tzdataVersion == "" {
		tzdataVersion = runtime.Version() + ":system-tzdata"
	}
	return OccurrenceCalculator{TZDataVersion: tzdataVersion}
}

func (c OccurrenceCalculator) Next(schedule storage.Schedule, after time.Time) (storage.ScheduleOccurrence, error) {
	if !validCron(schedule.Cron) {
		return storage.ScheduleOccurrence{}, ErrInvalidCron
	}
	loc, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return storage.ScheduleOccurrence{}, fmt.Errorf("%w: %v", ErrInvalidTimezone, err)
	}
	fields := strings.Fields(schedule.Cron)
	civil := time.Date(after.In(loc).Year(), after.In(loc).Month(), after.In(loc).Day(), after.In(loc).Hour(), after.In(loc).Minute(), 0, 0, time.UTC).Add(time.Minute)
	for i := 0; i < 60*24*366*10; i++ { // bounded search: at most ten years of civil minutes
		if cronMatches(fields, civil) {
			resolved, decision, ok := resolveCivilMinute(civil, loc)
			if ok && resolved.After(after) {
				return buildOccurrence(schedule, civil, resolved, decision, c.TZDataVersion), nil
			}
		}
		civil = civil.Add(time.Minute)
	}
	return storage.ScheduleOccurrence{}, ErrOccurrenceNotFound
}

func buildOccurrence(schedule storage.Schedule, civil, resolved time.Time, decision storage.DSTDecision, tzdata string) storage.ScheduleOccurrence {
	local := civil.Format("2006-01-02T15:04:05")
	identity := fmt.Sprintf("%s|%d|%s|%s|%s|%s", schedule.ID, schedule.Revision, local, resolved.UTC().Format(time.RFC3339), schedule.Timezone, decision)
	digest := sha256.Sum256([]byte(identity))
	key := hex.EncodeToString(digest[:])
	return storage.ScheduleOccurrence{
		ScheduleID: schedule.ID, ScheduleRevision: schedule.Revision, OccurrenceKey: key,
		LocalTime:  time.Date(civil.Year(), civil.Month(), civil.Day(), civil.Hour(), civil.Minute(), 0, 0, mustLocation(schedule.Timezone)),
		ResolvedAt: resolved.UTC(), Timezone: schedule.Timezone, TzdataVersion: tzdata,
		DSTDecision: decision, Status: storage.OccurrenceAdmitted,
		IdempotencyKey: schedule.ID + ":" + key, PolicyDigest: schedule.Instruction,
	}
}

func mustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

func resolveCivilMinute(civilUTC time.Time, loc *time.Location) (time.Time, storage.DSTDecision, bool) {
	y, month, d := civilUTC.Date()
	h, min, sec := civilUTC.Clock()
	initial := time.Date(y, month, d, h, min, sec, 0, loc)
	if initial.Year() != y || initial.Month() != month || initial.Day() != d || initial.Hour() != h || initial.Minute() != min {
		return time.Time{}, storage.DSTSkippedNonexistent, false
	}
	target := func(value time.Time) bool {
		vy, vm, vd := value.In(loc).Date()
		vh, vmin, _ := value.In(loc).Clock()
		return vy == y && vm == month && vd == d && vh == h && vmin == min
	}
	candidates := []time.Time{initial}
	seen := map[int64]bool{initial.UnixNano(): true}
	for offset := -36 * time.Hour; offset <= 36*time.Hour; offset += 15 * time.Minute {
		candidate := initial.Add(offset)
		if target(candidate) && !seen[candidate.UnixNano()] {
			seen[candidate.UnixNano()] = true
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) > 1 {
		earlier := candidates[0]
		for _, candidate := range candidates[1:] {
			if candidate.Before(earlier) {
				earlier = candidate
			}
		}
		return earlier.UTC(), storage.DSTEarlierAmbiguous, true
	}
	return initial.UTC(), storage.DSTNormal, true
}

func EffectKey(scheduleID, occurrenceKey, operation string) storage.EffectKey {
	return storage.EffectKey{Namespace: "schedule", Subject: scheduleID + ":" + occurrenceKey, Key: operation}
}

type MisfireResult struct {
	Occurrence storage.ScheduleOccurrence
	Decision   storage.DSTDecision
}

// ReconcileMisfires turns already resolved missed times into bounded records.
// It never invents additional times and keeps one stable identity per input.
func (c OccurrenceCalculator) ReconcileMisfires(schedule storage.Schedule, missed []time.Time) ([]MisfireResult, error) {
	if schedule.MisfirePolicy != storage.MisfireSkip && schedule.MisfirePolicy != storage.MisfireCoalesceLatest && schedule.MisfirePolicy != storage.MisfireBackfill {
		return nil, ErrInvalidMisfire
	}
	if schedule.MisfirePolicy == storage.MisfireCoalesceLatest && len(missed) > 1 {
		missed = missed[len(missed)-1:]
	}
	if schedule.MisfirePolicy == storage.MisfireBackfill && schedule.BackfillLimit >= 0 && len(missed) > schedule.BackfillLimit {
		missed = missed[:schedule.BackfillLimit]
	}
	results := make([]MisfireResult, 0, len(missed))
	for _, resolved := range missed {
		decision := storage.DSTBackfilled
		status := storage.OccurrenceAdmitted
		switch schedule.MisfirePolicy {
		case storage.MisfireSkip:
			decision, status = storage.DSTSkippedNonexistent, storage.OccurrenceSkipped
		case storage.MisfireCoalesceLatest:
			decision = storage.DSTCoalesced
		}
		identity := fmt.Sprintf("%s|%d|%s|%s", schedule.ID, schedule.Revision, resolved.UTC().Format(time.RFC3339), decision)
		digest := sha256.Sum256([]byte(identity))
		key := hex.EncodeToString(digest[:])
		results = append(results, MisfireResult{Occurrence: storage.ScheduleOccurrence{ScheduleID: schedule.ID, ScheduleRevision: schedule.Revision, OccurrenceKey: key, LocalTime: resolved.In(mustLocation(schedule.Timezone)), ResolvedAt: resolved.UTC(), Timezone: schedule.Timezone, TzdataVersion: c.TZDataVersion, DSTDecision: decision, Status: status, IdempotencyKey: schedule.ID + ":" + key}, Decision: decision})
	}
	return results, nil
}

func validCron(expr string) bool {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false
	}
	limits := [5][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}
	for i, field := range fields {
		if !validCronFieldTask(field, limits[i][0], limits[i][1]) {
			return false
		}
	}
	return true
}

func validCronFieldTask(field string, min, max int) bool {
	for _, part := range strings.Split(field, ",") {
		base := part
		if strings.Contains(base, "/") {
			pieces := strings.Split(base, "/")
			if len(pieces) != 2 || !isPositiveInt(pieces[1]) {
				return false
			}
			base = pieces[0]
		}
		if base == "*" {
			continue
		}
		if strings.Contains(base, "-") {
			pieces := strings.Split(base, "-")
			if len(pieces) != 2 {
				return false
			}
			start, e1 := strconv.Atoi(pieces[0])
			end, e2 := strconv.Atoi(pieces[1])
			if e1 != nil || e2 != nil || start < min || end > max || start > end {
				return false
			}
			continue
		}
		value, err := strconv.Atoi(base)
		if err != nil || value < min || value > max {
			return false
		}
	}
	return true
}

func isPositiveInt(value string) bool {
	n, err := strconv.Atoi(value)
	return err == nil && n > 0
}

func cronMatches(fields []string, civil time.Time) bool {
	values := []int{civil.Minute(), civil.Hour(), civil.Day(), int(civil.Month()), int(civil.Weekday())}
	limits := [5][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}
	matched := make([]bool, len(fields))
	for i, field := range fields {
		matched[i] = cronFieldMatches(field, values[i], limits[i][0], limits[i][1])
	}
	if !matched[0] || !matched[1] || !matched[3] {
		return false
	}
	dayOfMonthWildcard := strings.Contains(fields[2], "*")
	dayOfWeekWildcard := strings.Contains(fields[4], "*")
	if dayOfMonthWildcard || dayOfWeekWildcard {
		return matched[2] && matched[4]
	}
	return matched[2] || matched[4]
}

func cronFieldMatches(field string, value, min, max int) bool {
	for _, part := range strings.Split(field, ",") {
		base := part
		step := 1
		if strings.Contains(base, "/") {
			pieces := strings.Split(base, "/")
			base = pieces[0]
			step, _ = strconv.Atoi(pieces[1])
		}
		if base == "*" {
			return (value-min)%step == 0
		}
		if strings.Contains(base, "-") {
			pieces := strings.Split(base, "-")
			start, _ := strconv.Atoi(pieces[0])
			end, _ := strconv.Atoi(pieces[1])
			return value >= start && value <= end && (value-start)%step == 0
		}
		if n, err := strconv.Atoi(base); err == nil && n == value {
			return true
		}
	}
	return false
}
