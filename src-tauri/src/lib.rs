use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::fs;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::sync::{Arc, Mutex, MutexGuard};
use std::time::{Duration, SystemTime, UNIX_EPOCH};
use tauri::{AppHandle, Emitter, Manager, State};
use thiserror::Error;

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
enum SessionState {
    Starting,
    Running,
    Stopping,
    Exited,
    Failed,
    Cleaned,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct SessionInfo {
    id: u32,
    name: String,
    url: String,
    profile_dir: String,
    pid: u32,
    x: i32,
    y: i32,
    width: i32,
    height: i32,
    state: SessionState,
    error: String,
    started_at: Option<String>,
    ended_at: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct LaunchOptions {
    url: String,
    count: u32,
    base_name: String,
    chrome_path_override: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct Settings {
    chrome_path_override: Option<String>,
    base_name_default: String,
}

impl Default for Settings {
    fn default() -> Self {
        Self {
            chrome_path_override: None,
            base_name_default: "session".to_string(),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct ChromeDetectionResult {
    detected_path: Option<String>,
    source: String,
}

#[derive(Debug, Clone)]
struct RuntimeOptions {
    url: String,
    base_name: String,
    chrome_path: String,
    screen: ScreenBounds,
}

#[derive(Debug, Clone, Copy)]
struct ScreenBounds {
    width: i32,
    height: i32,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
struct WindowBounds {
    x: i32,
    y: i32,
    width: i32,
    height: i32,
}

#[derive(Debug)]
struct ManagedSession {
    info: SessionInfo,
}

#[derive(Clone, Default)]
struct SharedState {
    inner: Arc<Mutex<AppState>>,
}

#[derive(Default)]
struct AppState {
    next_id: u32,
    sessions: HashMap<u32, ManagedSession>,
    options: Option<RuntimeOptions>,
}

#[derive(Debug, Error)]
enum AppError {
    #[error("{0}")]
    Message(String),
    #[error("io error: {0}")]
    Io(#[from] std::io::Error),
    #[error("tauri error: {0}")]
    Tauri(#[from] tauri::Error),
    #[error("serialization error: {0}")]
    Serde(#[from] serde_json::Error),
}

type AppResult<T> = Result<T, String>;

impl From<AppError> for String {
    fn from(value: AppError) -> Self {
        value.to_string()
    }
}

fn lock_state<'a>(state: &'a SharedState) -> Result<MutexGuard<'a, AppState>, AppError> {
    state
        .inner
        .lock()
        .map_err(|_| AppError::Message("application state is poisoned".to_string()))
}

#[tauri::command]
fn get_sessions(state: State<'_, SharedState>) -> AppResult<Vec<SessionInfo>> {
    list_sessions(&state.inner).map_err(Into::into)
}

#[tauri::command]
fn get_settings(app: AppHandle) -> AppResult<Settings> {
    load_settings(&app).map_err(Into::into)
}

#[tauri::command]
fn save_settings(app: AppHandle, settings: Settings) -> AppResult<Settings> {
    persist_settings(&app, &settings).map_err(String::from)?;
    Ok(settings)
}

#[tauri::command]
fn detect_chrome(app: AppHandle) -> AppResult<ChromeDetectionResult> {
    let settings = load_settings(&app).unwrap_or_default();
    Ok(resolve_chrome_detection(settings.chrome_path_override))
}

#[tauri::command]
fn launch_sessions(
    app: AppHandle,
    state: State<'_, SharedState>,
    options: LaunchOptions,
) -> AppResult<Vec<SessionInfo>> {
    let settings = load_settings(&app).unwrap_or_default();
    validate_launch_options(&options)?;
    let screen = detect_screen(&app);
    let chrome_path =
        resolve_chrome_path(options.chrome_path_override.clone(), settings.chrome_path_override)?;

    {
        let mut guard = lock_state(&state).map_err(String::from)?;
        if active_session_count(&guard) > 0 {
            return Err("sessions are already running; add more or close the current batch".to_string());
        }
        if guard.next_id == 0 {
            guard.next_id = 1;
        }
        guard.options = Some(RuntimeOptions {
            url: options.url.trim().to_string(),
            base_name: options.base_name.trim().to_string(),
            chrome_path,
            screen,
        });
    }

    launch_more(&app, &state, options.count).map_err(Into::into)
}

#[tauri::command]
fn add_sessions(app: AppHandle, state: State<'_, SharedState>, count: u32) -> AppResult<Vec<SessionInfo>> {
    if count == 0 {
        return Err("count must be greater than zero".to_string());
    }
    launch_more(&app, &state, count).map_err(Into::into)
}

#[tauri::command]
fn terminate_session(
    app: AppHandle,
    state: State<'_, SharedState>,
    id: u32,
) -> AppResult<SessionInfo> {
    let pid = {
        let mut guard = lock_state(&state).map_err(String::from)?;
        let Some(session) = guard.sessions.get_mut(&id) else {
            return Err(format!("session {id} not found"));
        };
        if !is_active(&session.info.state) {
            return Err(format!("session {id} is not active"));
        }
        session.info.state = SessionState::Stopping;
        session.info.pid
    };

    emit_sessions(&app, &state.inner).map_err(String::from)?;
    terminate_process(pid).map_err(String::from)?;

    let guard = lock_state(&state).map_err(String::from)?;
    let session = guard
        .sessions
        .get(&id)
        .map(|item| item.info.clone())
        .ok_or_else(|| format!("session {id} not found"))?;
    Ok(session)
}

#[tauri::command]
fn terminate_all(app: AppHandle, state: State<'_, SharedState>) -> AppResult<()> {
    let pids = {
        let mut guard = lock_state(&state).map_err(String::from)?;
        let mut pids = Vec::new();
        for session in guard.sessions.values_mut() {
            if is_active(&session.info.state) {
                session.info.state = SessionState::Stopping;
                pids.push(session.info.pid);
            }
        }
        pids
    };

    emit_sessions(&app, &state.inner).map_err(String::from)?;

    for pid in pids {
        terminate_process(pid).map_err(String::from)?;
    }
    Ok(())
}

fn launch_more(app: &AppHandle, state: &SharedState, count: u32) -> Result<Vec<SessionInfo>, AppError> {
    let (options, starting_id, existing_ids) = {
        let mut guard = lock_state(state)?;
        let options = guard
            .options
            .clone()
            .ok_or_else(|| AppError::Message("launch a batch before adding sessions".to_string()))?;
        let starting_id = guard.next_id.max(1);
        let existing_ids = active_ids(&guard);
        guard.next_id = starting_id + count;
        (options, starting_id, existing_ids)
    };

    let total = existing_ids.len() as u32 + count;
    let tiles = tile_windows(total, options.screen);
    let mut retile_map = HashMap::new();
    for (index, id) in existing_ids.iter().enumerate() {
        if let Some(tile) = tiles.get(index).copied() {
            retile_map.insert(*id, tile);
        }
    }

    for offset in 0..count {
        let session_id = starting_id + offset;
        let tile = tiles[(existing_ids.len() as u32 + offset) as usize];
        let name = format!("{}-{}", options.base_name, session_id);
        let profile_dir = create_profile_dir(&name)?;
        let child = match spawn_chrome(&options.chrome_path, &profile_dir, &options.url, tile) {
            Ok(child) => child,
            Err(err) => {
                let _ = fs::remove_dir_all(&profile_dir);
                let message = err.to_string();
                let _ = app.emit("session-error", message.clone());
                return Err(AppError::Message(message));
            }
        };

        let info = SessionInfo {
            id: session_id,
            name,
            url: options.url.clone(),
            profile_dir: profile_dir.to_string_lossy().to_string(),
            pid: child.id(),
            x: tile.x,
            y: tile.y,
            width: tile.width,
            height: tile.height,
            state: SessionState::Running,
            error: String::new(),
            started_at: Some(now_stamp()),
            ended_at: None,
        };

        {
            let mut guard = lock_state(state)?;
            guard.sessions.insert(session_id, ManagedSession { info: info.clone() });
        }

        spawn_session_watcher(app.clone(), state.inner.clone(), session_id, child, profile_dir);
    }

    retile_active_sessions(app, &state.inner, retile_map)?;
    emit_sessions(app, &state.inner)?;
    list_sessions(&state.inner)
}

fn spawn_session_watcher(
    app: AppHandle,
    state: Arc<Mutex<AppState>>,
    session_id: u32,
    mut child: Child,
    profile_dir: PathBuf,
) {
    std::thread::spawn(move || {
        let wait_result = child.wait();
        let cleanup_error = cleanup_profile_dir(&profile_dir).err().map(|err| err.to_string());

        let (retile_map, error_message) = {
            let mut guard = match state.lock() {
                Ok(guard) => guard,
                Err(_) => return,
            };

            let mut error_message = None;
            if let Some(session) = guard.sessions.get_mut(&session_id) {
                session.info.ended_at = Some(now_stamp());
                match wait_result {
                    Ok(status) if status.success() => {
                        session.info.state = SessionState::Cleaned;
                        session.info.error.clear();
                    }
                    Ok(status) => {
                        session.info.state = SessionState::Failed;
                        session.info.error = format!("process exited with status {status}");
                    }
                    Err(err) => {
                        session.info.state = SessionState::Failed;
                        session.info.error = err.to_string();
                    }
                }

                if let Some(cleanup_error) = cleanup_error.clone() {
                    session.info.state = SessionState::Failed;
                    if session.info.error.is_empty() {
                        session.info.error = cleanup_error.clone();
                    } else {
                        session.info.error = format!("{} | {}", session.info.error, cleanup_error);
                    }
                }

                if !session.info.error.is_empty() {
                    error_message = Some(session.info.error.clone());
                }
            }

            let retile_map = build_retile_map(&mut guard);
            (retile_map, error_message)
        };

        let _ = retile_active_sessions(&app, &state, retile_map);
        let _ = emit_sessions(&app, &state);
        if let Some(message) = error_message {
            let _ = app.emit("session-error", message);
        }
    });
}

fn emit_sessions(app: &AppHandle, state: &Arc<Mutex<AppState>>) -> Result<(), AppError> {
    let sessions = list_sessions(state)?;
    app.emit("sessions-updated", sessions)?;
    Ok(())
}

fn list_sessions(state: &Arc<Mutex<AppState>>) -> Result<Vec<SessionInfo>, AppError> {
    let guard = state
        .lock()
        .map_err(|_| AppError::Message("application state is poisoned".to_string()))?;
    Ok(sorted_sessions(&guard))
}

fn sorted_sessions(state: &AppState) -> Vec<SessionInfo> {
    let mut sessions = state
        .sessions
        .values()
        .map(|session| session.info.clone())
        .collect::<Vec<_>>();
    sessions.sort_by_key(|session| session.id);
    sessions
}

fn build_retile_map(state: &mut AppState) -> HashMap<u32, WindowBounds> {
    let Some(options) = state.options.clone() else {
        return HashMap::new();
    };

    let ids = active_ids(state);
    let tiles = tile_windows(ids.len() as u32, options.screen);
    let mut map = HashMap::new();

    for (index, id) in ids.iter().enumerate() {
        if let Some(tile) = tiles.get(index).copied() {
            if let Some(session) = state.sessions.get_mut(id) {
                session.info.x = tile.x;
                session.info.y = tile.y;
                session.info.width = tile.width;
                session.info.height = tile.height;
            }
            map.insert(*id, tile);
        }
    }

    map
}

fn retile_active_sessions(
    app: &AppHandle,
    state: &Arc<Mutex<AppState>>,
    updates: HashMap<u32, WindowBounds>,
) -> Result<(), AppError> {
    if updates.is_empty() {
        return Ok(());
    }

    let sessions = {
        let mut guard = state
            .lock()
            .map_err(|_| AppError::Message("application state is poisoned".to_string()))?;
        let mut sessions = Vec::new();
        for (id, tile) in updates {
            if let Some(session) = guard.sessions.get_mut(&id) {
                session.info.x = tile.x;
                session.info.y = tile.y;
                session.info.width = tile.width;
                session.info.height = tile.height;
                sessions.push((session.info.pid, tile));
            }
        }
        sessions.sort_by_key(|entry| entry.0);
        sessions
    };

    if sessions.is_empty() {
        return Ok(());
    }

    platform::retile(&sessions)?;
    emit_sessions(app, state)?;
    Ok(())
}

fn active_ids(state: &AppState) -> Vec<u32> {
    let mut ids = state
        .sessions
        .values()
        .filter(|session| is_active(&session.info.state))
        .map(|session| session.info.id)
        .collect::<Vec<_>>();
    ids.sort_unstable();
    ids
}

fn active_session_count(state: &AppState) -> usize {
    state
        .sessions
        .values()
        .filter(|session| is_active(&session.info.state))
        .count()
}

fn is_active(state: &SessionState) -> bool {
    matches!(
        state,
        SessionState::Starting | SessionState::Running | SessionState::Stopping
    )
}

fn validate_launch_options(options: &LaunchOptions) -> AppResult<()> {
    if options.url.trim().is_empty() {
        return Err("url is required".to_string());
    }
    if options.count == 0 {
        return Err("count must be greater than zero".to_string());
    }
    if options.base_name.trim().is_empty() {
        return Err("base name is required".to_string());
    }
    Ok(())
}

fn create_profile_dir(name: &str) -> Result<PathBuf, AppError> {
    let stamp = now_stamp();
    let profile_dir = std::env::temp_dir().join(format!("multibrowser-{name}-{stamp}"));
    fs::create_dir_all(&profile_dir)?;
    Ok(profile_dir)
}

fn cleanup_profile_dir(profile_dir: &Path) -> Result<(), AppError> {
    if profile_dir.exists() {
        fs::remove_dir_all(profile_dir)?;
    }
    Ok(())
}

fn spawn_chrome(
    chrome_path: &str,
    profile_dir: &Path,
    url: &str,
    bounds: WindowBounds,
) -> Result<Child, AppError> {
    if !Path::new(chrome_path).exists() {
        return Err(AppError::Message(format!(
            "chrome binary not found at \"{chrome_path}\""
        )));
    }

    let mut command = Command::new(chrome_path);
    command
        .arg("--no-first-run")
        .arg("--no-default-browser-check")
        .arg("--new-window")
        .arg(format!("--user-data-dir={}", profile_dir.display()))
        .arg(format!("--window-position={},{}", bounds.x, bounds.y))
        .arg(format!("--window-size={},{}", bounds.width, bounds.height))
        .arg(url)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null());

    command.spawn().map_err(AppError::from)
}

fn detect_screen(app: &AppHandle) -> ScreenBounds {
    let fallback = ScreenBounds {
        width: 1440,
        height: 900,
    };

    match app.primary_monitor() {
        Ok(Some(monitor)) => {
            let size = monitor.size();
            ScreenBounds {
                width: size.width as i32,
                height: size.height as i32,
            }
        }
        _ => fallback,
    }
}

fn tile_windows(count: u32, screen: ScreenBounds) -> Vec<WindowBounds> {
    if count == 0 || screen.width <= 0 || screen.height <= 0 {
        return Vec::new();
    }

    let cols = (count as f64).sqrt().ceil() as i32;
    let rows = (count as f64 / cols as f64).ceil() as i32;
    let mut output = Vec::with_capacity(count as usize);

    for index in 0..count as i32 {
        let row = index / cols;
        let col = index % cols;
        let x = screen.width * col / cols;
        let y = screen.height * row / rows;
        let next_x = screen.width * (col + 1) / cols;
        let next_y = screen.height * (row + 1) / rows;
        output.push(WindowBounds {
            x,
            y,
            width: next_x - x,
            height: next_y - y,
        });
    }

    output
}

fn now_stamp() -> String {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or(Duration::from_secs(0))
        .as_millis()
        .to_string()
}

fn settings_path(app: &AppHandle) -> Result<PathBuf, AppError> {
    let dir = app.path().app_config_dir()?;
    fs::create_dir_all(&dir)?;
    Ok(dir.join("settings.json"))
}

fn load_settings(app: &AppHandle) -> Result<Settings, AppError> {
    let path = settings_path(app)?;
    if !path.exists() {
        return Ok(Settings::default());
    }
    let contents = fs::read_to_string(path)?;
    Ok(serde_json::from_str(&contents)?)
}

fn persist_settings(app: &AppHandle, settings: &Settings) -> Result<(), AppError> {
    let path = settings_path(app)?;
    let contents = serde_json::to_string_pretty(settings)?;
    fs::write(path, contents)?;
    Ok(())
}

fn resolve_chrome_path(
    override_path: Option<String>,
    stored_override: Option<String>,
) -> Result<String, AppError> {
    let detection = resolve_chrome_detection(override_path.or(stored_override));
    detection.detected_path.ok_or_else(|| {
        AppError::Message("chrome could not be detected; set a path override".to_string())
    })
}

fn resolve_chrome_detection(preferred_override: Option<String>) -> ChromeDetectionResult {
    if let Some(path) = preferred_override
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
    {
        if Path::new(&path).exists() {
            return ChromeDetectionResult {
                detected_path: Some(path),
                source: "override".to_string(),
            };
        }
    }

    #[cfg(target_os = "macos")]
    {
        let candidates = vec![
            "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome".to_string(),
            format!(
                "{}/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
                std::env::var("HOME").unwrap_or_default()
            ),
        ];
        for candidate in candidates {
            if Path::new(&candidate).exists() {
                return ChromeDetectionResult {
                    detected_path: Some(candidate),
                    source: "macos-default".to_string(),
                };
            }
        }
    }

    #[cfg(target_os = "windows")]
    {
        let mut candidates = Vec::new();
        if let Ok(program_files) = std::env::var("ProgramFiles") {
            candidates.push(format!(r"{program_files}\Google\Chrome\Application\chrome.exe"));
        }
        if let Ok(program_files_x86) = std::env::var("ProgramFiles(x86)") {
            candidates.push(format!(
                r"{program_files_x86}\Google\Chrome\Application\chrome.exe"
            ));
        }
        if let Ok(local_app_data) = std::env::var("LOCALAPPDATA") {
            candidates.push(format!(
                r"{local_app_data}\Google\Chrome\Application\chrome.exe"
            ));
        }
        for candidate in candidates {
            if Path::new(&candidate).exists() {
                return ChromeDetectionResult {
                    detected_path: Some(candidate),
                    source: "windows-default".to_string(),
                };
            }
        }
    }

    ChromeDetectionResult {
        detected_path: None,
        source: "not-found".to_string(),
    }
}

fn terminate_process(pid: u32) -> Result<(), AppError> {
    #[cfg(target_os = "windows")]
    {
        let status = Command::new("taskkill")
            .args(["/PID", &pid.to_string(), "/T", "/F"])
            .status()?;
        if status.success() {
            return Ok(());
        }
        return Err(AppError::Message(format!("failed to terminate process {pid}")));
    }

    #[cfg(not(target_os = "windows"))]
    {
        let status = Command::new("kill")
            .args(["-TERM", &pid.to_string()])
            .status()?;
        if status.success() {
            return Ok(());
        }
        let force = Command::new("kill")
            .args(["-KILL", &pid.to_string()])
            .status()?;
        if force.success() {
            return Ok(());
        }
        Err(AppError::Message(format!("failed to terminate process {pid}")))
    }
}

mod platform {
    use super::{AppError, WindowBounds};

    pub fn retile(sessions: &[(u32, WindowBounds)]) -> Result<(), AppError> {
        #[cfg(target_os = "macos")]
        {
            return retile_macos(sessions);
        }

        #[cfg(target_os = "windows")]
        {
            return retile_windows(sessions);
        }

        #[allow(unreachable_code)]
        Ok(())
    }

    #[cfg(target_os = "macos")]
    fn retile_macos(sessions: &[(u32, WindowBounds)]) -> Result<(), AppError> {
        use std::process::Command;

        if sessions.is_empty() {
            return Ok(());
        }

        let mut lines = vec![
            "tell application \"Google Chrome\"".to_string(),
            "if not running then return".to_string(),
        ];

        for (index, (_, bounds)) in sessions.iter().enumerate() {
            lines.push(format!("if (count of windows) >= {} then", index + 1));
            lines.push(format!(
                "set bounds of window {} to {{{}, {}, {}, {}}}",
                index + 1,
                bounds.x,
                bounds.y,
                bounds.x + bounds.width,
                bounds.y + bounds.height
            ));
            lines.push("end if".to_string());
        }

        lines.push("end tell".to_string());
        let status = Command::new("osascript")
            .arg("-e")
            .arg(lines.join("\n"))
            .status()?;
        if status.success() {
            Ok(())
        } else {
            Err(AppError::Message(
                "failed to retile Chrome windows on macOS".to_string(),
            ))
        }
    }

    #[cfg(target_os = "windows")]
    fn retile_windows(sessions: &[(u32, WindowBounds)]) -> Result<(), AppError> {
        use windows_sys::Win32::Foundation::{BOOL, HWND, LPARAM};
        use windows_sys::Win32::UI::WindowsAndMessaging::{
            EnumWindows, GetWindowThreadProcessId, IsWindowVisible, MoveWindow, ShowWindow,
            SW_RESTORE,
        };

        struct EnumData {
            target_pid: u32,
            matches: Vec<HWND>,
        }

        unsafe extern "system" fn enum_windows(hwnd: HWND, lparam: LPARAM) -> BOOL {
            let data = &mut *(lparam as *mut EnumData);
            let mut pid = 0;
            GetWindowThreadProcessId(hwnd, &mut pid);
            if pid == data.target_pid && IsWindowVisible(hwnd) != 0 {
                data.matches.push(hwnd);
            }
            1
        }

        for (pid, bounds) in sessions {
            let mut data = EnumData {
                target_pid: *pid,
                matches: Vec::new(),
            };
            unsafe {
                EnumWindows(Some(enum_windows), &mut data as *mut EnumData as isize);
                for hwnd in data.matches {
                    ShowWindow(hwnd, SW_RESTORE);
                    MoveWindow(hwnd, bounds.x, bounds.y, bounds.width, bounds.height, 1);
                }
            }
        }

        Ok(())
    }
}

pub fn run() {
    tauri::Builder::default()
        .manage(SharedState::default())
        .invoke_handler(tauri::generate_handler![
            get_sessions,
            get_settings,
            save_settings,
            detect_chrome,
            launch_sessions,
            add_sessions,
            terminate_session,
            terminate_all
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn tiles_single_window() {
        let tiles = tile_windows(
            1,
            ScreenBounds {
                width: 1440,
                height: 900,
            },
        );
        assert_eq!(
            tiles[0],
            WindowBounds {
                x: 0,
                y: 0,
                width: 1440,
                height: 900,
            }
        );
    }

    #[test]
    fn tiles_three_windows_in_grid() {
        let tiles = tile_windows(
            3,
            ScreenBounds {
                width: 1200,
                height: 900,
            },
        );
        assert_eq!(tiles.len(), 3);
        assert_eq!(
            tiles[0],
            WindowBounds {
                x: 0,
                y: 0,
                width: 600,
                height: 450,
            }
        );
        assert_eq!(
            tiles[2],
            WindowBounds {
                x: 0,
                y: 450,
                width: 600,
                height: 450,
            }
        );
    }

    #[test]
    fn override_detection_wins() {
        let temp = tempfile_like_path("chrome");
        fs::write(&temp, "binary").unwrap();
        let detection = resolve_chrome_detection(Some(temp.to_string_lossy().to_string()));
        assert_eq!(detection.source, "override");
        assert_eq!(
            detection.detected_path,
            Some(temp.to_string_lossy().to_string())
        );
        fs::remove_file(temp).unwrap();
    }

    #[test]
    fn validation_rejects_missing_fields() {
        let err = validate_launch_options(&LaunchOptions {
            url: "".to_string(),
            count: 0,
            base_name: "".to_string(),
            chrome_path_override: None,
        })
        .unwrap_err();
        assert_eq!(err, "url is required");
    }

    fn tempfile_like_path(name: &str) -> PathBuf {
        std::env::temp_dir().join(format!("multibrowser-test-{name}-{}", now_stamp()))
    }
}
