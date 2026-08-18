import { useEffect, useRef, useState } from "react";
import { api, type JobState } from "./api";

// Live state of the one background task an instance can have. The stream sends
// the current state as its first event, so a page opened mid-import shows the
// bar straight away instead of an empty form; EventSource reconnects on its own,
// and the snapshot endpoint covers the case where the stream cannot open at all.
export function useJob(onFinish?: () => void) {
  const [job, setJob] = useState<JobState | null>(null);
  const finish = useRef(onFinish);
  finish.current = onFinish;
  const wasRunning = useRef(false);

  useEffect(() => {
    const es = new EventSource("/api/job/stream");
    es.onmessage = (e) => setJob(JSON.parse(e.data) as JobState);
    es.onerror = () => {
      api
        .job()
        .then(setJob)
        .catch(() => {});
    };
    return () => es.close();
  }, []);

  useEffect(() => {
    if (!job) return;
    if (wasRunning.current && !job.running) finish.current?.();
    wasRunning.current = job.running;
  }, [job]);

  return job;
}
