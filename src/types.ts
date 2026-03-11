export type SessionState =
  | "starting"
  | "running"
  | "stopping"
  | "exited"
  | "failed"
  | "cleaned";

export interface SessionInfo {
  id: number;
  name: string;
  url: string;
  profile_dir: string;
  pid: number;
  x: number;
  y: number;
  width: number;
  height: number;
  state: SessionState;
  error: string;
  started_at?: string | null;
  ended_at?: string | null;
}

export interface LaunchOptions {
  url: string;
  count: number;
  base_name: string;
  chrome_path_override?: string | null;
}

export interface Settings {
  chrome_path_override?: string | null;
  base_name_default: string;
}

export interface ChromeDetectionResult {
  detected_path?: string | null;
  source: string;
}
