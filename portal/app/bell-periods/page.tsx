"use client";

import { useEffect, useState } from "react";
import { RequireSession } from "../../components/RequireSession";
import { apiGet, apiSend, type CalendarResponse } from "../../lib/api";

type BreakRow = { label: string; start_time: string; end_time: string };

const dayOptions = ["MONDAY", "TUESDAY", "WEDNESDAY", "THURSDAY", "FRIDAY"];

export default function BellPeriodsPage() {
  const [learningDays, setLearningDays] = useState<string[]>(dayOptions);
  const [dayStartTime, setDayStartTime] = useState("08:00");
  const [dayEndTime, setDayEndTime] = useState("15:30");
  const [slotDurationMinutes, setSlotDurationMinutes] = useState(40);
  const [breaks, setBreaks] = useState<BreakRow[]>([
    { label: "First break", start_time: "10:00", end_time: "10:20" },
    { label: "Lunch break", start_time: "12:40", end_time: "13:20" },
  ]);
  const [calendar, setCalendar] = useState<CalendarResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  function load() {
    apiGet<CalendarResponse>("/api/v1/calendar")
      .then(setCalendar)
      .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
  }

  useEffect(() => {
    load();
  }, []);

  return (
    <RequireSession>
      {() => (
        <section className="card">
          <h2>Bell-period configuration</h2>
          <p className="muted">Configure plugin-owned period structure for the synchronized active workspace. Term dates remain owned by Scholaroscope.</p>

          <div className="grid">
            <section className="card">
              <h3>Learning days</h3>
              {dayOptions.map((day) => (
                <label key={day} className="check-row">
                  <input
                    type="checkbox"
                    checked={learningDays.includes(day)}
                    onChange={(event) => {
                      setLearningDays((current) => event.target.checked
                        ? [...current, day]
                        : current.filter((value) => value !== day));
                    }}
                  />
                  {day}
                </label>
              ))}
            </section>
            <section className="card">
              <h3>Day bounds</h3>
              <label>Start <input type="time" value={dayStartTime} onChange={(event) => setDayStartTime(event.target.value)} /></label>
              <label>End <input type="time" value={dayEndTime} onChange={(event) => setDayEndTime(event.target.value)} /></label>
              <label>Teaching period length <input type="number" min={5} value={slotDurationMinutes} onChange={(event) => setSlotDurationMinutes(Number(event.target.value))} /> minutes</label>
            </section>
          </div>

          <section className="card">
            <h3>Breaks and non-teaching periods</h3>
            {breaks.map((item, index) => (
              <div className="toolbar" key={`${item.label}-${index}`}>
                <input value={item.label} onChange={(event) => setBreaks((rows) => rows.map((row, i) => i === index ? { ...row, label: event.target.value } : row))} placeholder="Break label" />
                <input type="time" value={item.start_time} onChange={(event) => setBreaks((rows) => rows.map((row, i) => i === index ? { ...row, start_time: event.target.value } : row))} />
                <input type="time" value={item.end_time} onChange={(event) => setBreaks((rows) => rows.map((row, i) => i === index ? { ...row, end_time: event.target.value } : row))} />
                <button type="button" onClick={() => setBreaks((rows) => rows.filter((_, i) => i !== index))}>Remove</button>
              </div>
            ))}
            <button type="button" onClick={() => setBreaks((rows) => [...rows, { label: "Break", start_time: "10:00", end_time: "10:20" }])}>Add break</button>
          </section>

          <div className="toolbar">
            <button
              type="button"
              onClick={() => {
                setError(null);
                apiSend<CalendarResponse>("/api/v1/calendar", "PUT", {
                  learning_days: learningDays,
                  day_start_time: dayStartTime,
                  day_end_time: dayEndTime,
                  slot_duration_minutes: slotDurationMinutes,
                  break_structure: breaks,
                })
                  .then((response) => setCalendar(response))
                  .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed"));
              }}
            >
              Save and activate calendar
            </button>
          </div>

          {error ? <div className="error">{error}</div> : null}
          <section className="card">
            <h3>Active periods</h3>
            {!calendar?.slots?.length ? <p className="muted">No active calendar has been configured.</p> : (
              <table>
                <thead><tr><th>Day</th><th>Start</th><th>End</th><th>Type</th></tr></thead>
                <tbody>
                  {calendar.slots.map((slot, index) => {
                    const row = slot as { day_of_week?: number; start_time?: string; end_time?: string; slot_type?: string };
                    return <tr key={index}><td>{row.day_of_week}</td><td>{row.start_time}</td><td>{row.end_time}</td><td>{row.slot_type}</td></tr>;
                  })}
                </tbody>
              </table>
            )}
          </section>
        </section>
      )}
    </RequireSession>
  );
}
