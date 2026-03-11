import { FormEvent, useEffect, useMemo, useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { listen, UnlistenFn } from "@tauri-apps/api/event";
import {
  ChromeDetectionResult,
  LaunchOptions,
  SessionInfo,
  Settings
} from "./types";

const defaultSettings: Settings = {
  chrome_path_override: "",
  base_name_default: "session"
};

function App() {
  const [sessions, setSessions] = useState<SessionInfo[]>([]);
  const [settings, setSettings] = useState<Settings>(defaultSettings);
  const [url, setUrl] = useState("https://example.com");
  const [count, setCount] = useState("3");
  const [baseName, setBaseName] = useState(defaultSettings.base_name_default);
  const [addCount, setAddCount] = useState("1");
  const [chromePath, setChromePath] = useState("");
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [notice, setNotice] = useState<string>("");
  const [detectedChrome, setDetectedChrome] = useState<ChromeDetectionResult | null>(null);

  const activeSessions = useMemo(
    () => sessions.filter((session) => ["starting", "running", "stopping"].includes(session.state)),
    [sessions]
  );

  useEffect(() => {
    let mounted = true;
    let unlistenSessions: UnlistenFn | undefined;
    let unlistenErrors: UnlistenFn | undefined;

    const setup = async () => {
      const [sessionList, loadedSettings, detection] = await Promise.all([
        invoke<SessionInfo[]>("get_sessions"),
        invoke<Settings>("get_settings"),
        invoke<ChromeDetectionResult>("detect_chrome")
      ]);

      if (!mounted) {
        return;
      }

      setSessions(sessionList);
      setSettings(loadedSettings);
      setBaseName(loadedSettings.base_name_default);
      setChromePath(loadedSettings.chrome_path_override ?? "");
      setDetectedChrome(detection);

      unlistenSessions = await listen<SessionInfo[]>("sessions-updated", (event) => {
        setSessions(event.payload);
      });

      unlistenErrors = await listen<string>("session-error", (event) => {
        setNotice(event.payload);
      });
    };

    void setup().catch((error) => {
      setNotice(String(error));
    });

    return () => {
      mounted = false;
      if (unlistenSessions) {
        void unlistenSessions();
      }
      if (unlistenErrors) {
        void unlistenErrors();
      }
    };
  }, []);

  async function runAction<T>(label: string, work: () => Promise<T>) {
    setBusy(label);
    setNotice("");
    try {
      return await work();
    } catch (error) {
      setNotice(error instanceof Error ? error.message : String(error));
      throw error;
    } finally {
      setBusy(null);
    }
  }

  async function handleLaunch(event: FormEvent) {
    event.preventDefault();

    const payload: LaunchOptions = {
      url: url.trim(),
      count: Number(count),
      base_name: baseName.trim() || "session",
      chrome_path_override: chromePath.trim() || null
    };

    await runAction("launching", async () => {
      const nextSessions = await invoke<SessionInfo[]>("launch_sessions", { options: payload });
      setSessions(nextSessions);
      setSelectedId(nextSessions[0]?.id ?? null);

      const nextSettings = await invoke<Settings>("save_settings", {
        settings: {
          chrome_path_override: payload.chrome_path_override,
          base_name_default: payload.base_name
        }
      });
      setSettings(nextSettings);
      setNotice(`Opened ${payload.count} Chrome sessions.`);
    });
  }

  async function handleAdd() {
    await runAction("adding", async () => {
      const extra = Number(addCount);
      const nextSessions = await invoke<SessionInfo[]>("add_sessions", { count: extra });
      setSessions(nextSessions);
      setNotice(`Added ${extra} sessions.`);
    });
  }

  async function handleCloseSelected() {
    if (selectedId == null) {
      return;
    }

    await runAction("closing", async () => {
      await invoke<SessionInfo>("terminate_session", { id: selectedId });
      setNotice(`Closing session ${selectedId}.`);
    });
  }

  async function handleQuitAll() {
    await runAction("quitting", async () => {
      await invoke("terminate_all");
      setNotice("Closing all managed sessions.");
    });
  }

  async function handleDetectChrome() {
    await runAction("detecting", async () => {
      const detection = await invoke<ChromeDetectionResult>("detect_chrome");
      setDetectedChrome(detection);
      if (detection.detected_path) {
        setChromePath(detection.detected_path);
        setNotice(`Detected Chrome at ${detection.detected_path}.`);
      } else {
        setNotice("Chrome was not found automatically.");
      }
    });
  }

  async function handleSaveSettings() {
    await runAction("saving", async () => {
      const next = await invoke<Settings>("save_settings", {
        settings: {
          chrome_path_override: chromePath.trim() || null,
          base_name_default: baseName.trim() || "session"
        }
      });
      setSettings(next);
      setNotice("Settings saved.");
    });
  }

  return (
    <main className="max-w-6xl mx-auto w-full space-y-8" data-purpose="main-application-container">
      {/* BEGIN: HeaderSection */}
      <header className="flex flex-col md:flex-row justify-between items-start md:items-center gap-6" data-purpose="app-header">
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <span className="px-2 py-0.5 text-[10px] font-bold tracking-widest uppercase bg-electric-blue/20 text-electric-blue rounded border border-electric-blue/30">
              v2.0 Stable
            </span>
            <span className="text-xs text-slate-500 font-medium tracking-wide uppercase">Desktop Control Room</span>
          </div>
          <h1 className="text-5xl font-extrabold tracking-tight bg-gradient-to-r from-white via-slate-200 to-slate-500 bg-clip-text text-transparent italic">
            multibrowser
          </h1>
          <p className="text-slate-400 max-w-xl text-sm leading-relaxed">
            Launch isolated Chrome windows, tile them across the screen, and manage them from one unified desktop surface.
          </p>
        </div>

        {/* Quick Stats Cards */}
        <div className="flex gap-4 w-full md:w-auto" data-purpose="header-stats">
          <div className="glass-panel p-4 rounded-2xl border border-charcoal-border flex-1 min-w-[120px]">
            <p className="text-[10px] font-semibold text-slate-500 uppercase tracking-wider mb-1">Active</p>
            <div className="flex items-baseline gap-2">
              <span className="text-3xl font-bold text-white">{activeSessions.length}</span>
              <span className="w-2 h-2 rounded-full bg-green-500 animate-pulse"></span>
            </div>
          </div>
          <div className="glass-panel p-4 rounded-2xl border border-charcoal-border flex-1 min-w-[120px]">
            <p className="text-[10px] font-semibold text-slate-500 uppercase tracking-wider mb-1">Tracked</p>
            <span className="text-3xl font-bold text-white">{sessions.length}</span>
          </div>
          <div className="glass-panel p-4 rounded-2xl border border-charcoal-border flex-1 min-w-[140px] relative overflow-hidden">
            <p className="text-[10px] font-semibold text-slate-500 uppercase tracking-wider mb-1">Chrome Status</p>
            <span className="text-2xl font-bold text-electric-blue">{detectedChrome?.detected_path ? "Ready" : "Check path"}</span>
            <div className="absolute -right-4 -bottom-4 w-12 h-12 bg-electric-blue/10 blur-2xl rounded-full"></div>
          </div>
        </div>
      </header>
      {/* END: HeaderSection */}

      {/* BEGIN: LaunchControlCenter */}
      <section className="glass-panel rounded-3xl p-8 border border-charcoal-border shadow-2xl" data-purpose="launch-configuration">
        <div className="flex justify-between items-center mb-8">
          <div>
            <h2 className="text-xl font-bold text-white tracking-tight">Launch Batch</h2>
            <p className="text-sm text-slate-500">Configure parameters for your new browsing session.</p>
          </div>
          <button onClick={() => void handleDetectChrome()} disabled={busy !== null} className="px-4 py-2 text-xs font-semibold bg-charcoal-card border border-slate-700 text-slate-300 rounded-lg hover:bg-slate-800 transition-all flex items-center gap-2">
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" strokeLinecap="round" strokeLinejoin="round" strokeWidth="2"></path></svg>
            Detect Chrome
          </button>
        </div>

        <form className="grid grid-cols-1 md:grid-cols-12 gap-6" onSubmit={(event) => void handleLaunch(event)}>
          {/* URL Input */}
          <div className="md:col-span-6 space-y-2">
            <label className="block text-xs font-medium text-slate-400 uppercase tracking-widest" htmlFor="url-input">Target URL</label>
            <input 
              value={url} 
              onChange={(event) => setUrl(event.target.value)} 
              className="w-full bg-charcoal-dark/50 border-slate-800 rounded-xl px-4 py-3 text-white focus:ring-2 focus:ring-neon-purple/50 focus:border-neon-purple transition-all placeholder:text-slate-600" 
              id="url-input" 
              placeholder="https://example.com" 
              type="text" 
            />
          </div>

          {/* Instance Count */}
          <div className="md:col-span-2 space-y-2">
            <label className="block text-xs font-medium text-slate-400 uppercase tracking-widest" htmlFor="count-input">Instance Count</label>
            <input 
              value={count}
              onChange={(event) => setCount(event.target.value)}
              className="w-full bg-charcoal-dark/50 border-slate-800 rounded-xl px-4 py-3 text-white focus:ring-2 focus:ring-neon-purple/50 focus:border-neon-purple transition-all" 
              id="count-input" 
              max="50" 
              min="1" 
              type="number" 
            />
          </div>

          {/* Base Name */}
          <div className="md:col-span-4 space-y-2">
            <label className="block text-xs font-medium text-slate-400 uppercase tracking-widest" htmlFor="name-input">Session Base Name</label>
            <input 
              value={baseName} 
              onChange={(event) => setBaseName(event.target.value)}
              className="w-full bg-charcoal-dark/50 border-slate-800 rounded-xl px-4 py-3 text-white focus:ring-2 focus:ring-neon-purple/50 focus:border-neon-purple transition-all placeholder:text-slate-600" 
              id="name-input" 
              placeholder="session_alpha" 
              type="text"
            />
          </div>

          {/* Path Override */}
          <div className="md:col-span-12 space-y-2">
            <label className="block text-xs font-medium text-slate-400 uppercase tracking-widest" htmlFor="path-input">Chrome Binary Path (Optional)</label>
            <input 
              value={chromePath}
              onChange={(event) => setChromePath(event.target.value)}
              className="w-full bg-charcoal-dark/50 border-slate-800 rounded-xl px-4 py-3 text-xs font-mono text-slate-400 focus:ring-2 focus:ring-neon-purple/50 focus:border-neon-purple transition-all placeholder:text-slate-700" 
              id="path-input" 
              placeholder={detectedChrome?.detected_path ?? "Use detected installation"} 
              type="text"
            />
          </div>

          {/* Action Buttons */}
          <div className="md:col-span-12 flex flex-wrap gap-4 pt-4">
            <button type="submit" disabled={busy !== null} className="bg-gradient-to-r from-neon-purple to-purple-700 hover:from-purple-500 hover:to-purple-600 text-white font-bold py-3 px-8 rounded-xl shadow-glow-purple transition-all transform active:scale-95 flex items-center gap-3">
              <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 20 20"><path clipRule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM9.555 7.168A1 1 0 008 8v4a1 1 0 001.555.832l3-2a1 1 0 000-1.664l-3-2z" fillRule="evenodd"></path></svg>
              {busy === "launching" ? "Launching..." : "Launch Sessions"}
            </button>
            <button type="button" onClick={() => void handleSaveSettings()} disabled={busy !== null} className="bg-slate-800/50 hover:bg-slate-700 text-slate-300 font-semibold py-3 px-8 rounded-xl border border-slate-700 transition-all">
              Save Defaults
            </button>
          </div>
        </form>
      </section>
      {/* END: LaunchControlCenter */}

      {/* BEGIN: RuntimeSection */}
      <section className="space-y-4" data-purpose="runtime-sessions-list">
        <div className="flex flex-col md:flex-row justify-between md:items-end gap-4">
          <div className="flex flex-col sm:flex-row items-start sm:items-center gap-4">
            <h2 className="text-sm font-bold text-slate-400 uppercase tracking-widest">Active Runtime</h2>
            <span className="px-2 py-0.5 text-[10px] bg-emerald-500/10 text-emerald-500 rounded border border-emerald-500/20 font-bold uppercase">{activeSessions.length} Sessions Active</span>
          </div>
          <div className="flex flex-wrap gap-2">
            <div className="flex items-center gap-2">
              <input 
                type="number" 
                min="1" 
                value={addCount} 
                onChange={(event) => setAddCount(event.target.value)}
                className="w-16 h-7 bg-charcoal-dark/50 border-slate-800 rounded px-2 text-xs text-white" 
              />
              <button onClick={() => void handleAdd()} disabled={busy !== null} className="text-[10px] text-slate-500 hover:text-white uppercase font-bold tracking-tighter bg-charcoal-card px-3 py-1 rounded-md border border-charcoal-border">
                {busy === "adding" ? "..." : "Add"}
              </button>
            </div>
            <button onClick={() => void handleCloseSelected()} disabled={busy !== null || selectedId == null} className="text-[10px] text-slate-500 hover:text-white uppercase font-bold tracking-tighter bg-charcoal-card px-3 py-1 rounded-md border border-charcoal-border">
              Close Selected
            </button>
            <button onClick={() => void handleQuitAll()} disabled={busy !== null || activeSessions.length === 0} className="text-[10px] text-red-500 hover:text-red-400 uppercase font-bold tracking-tighter bg-charcoal-card px-3 py-1 rounded-md border border-charcoal-border">
               {busy === "quitting" ? "Closing..." : "Terminate All"}
            </button>
          </div>
        </div>

        {/* Runtime Session List */}
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          {sessions.length === 0 ? (
            <div className="col-span-full py-12 text-center text-slate-500 border border-dashed border-charcoal-border rounded-2xl">
              No sessions yet. Launch a batch to begin.
            </div>
          ) : (
            sessions.map((session) => {
              const isActive = ["starting", "running"].includes(session.state);
              const statusColor = isActive ? "green" : session.state === "failed" ? "red" : "yellow";
              const isSelected = selectedId === session.id;
              
              return (
                <div 
                  key={session.id} 
                  onClick={() => setSelectedId(session.id)}
                  className={`glass-panel border ${isSelected ? 'border-neon-purple shadow-glow-purple' : 'border-charcoal-border hover:border-neon-purple/40'} p-4 rounded-2xl transition-all cursor-pointer group`}
                >
                  <div className="flex justify-between items-start mb-3">
                    <div className="w-8 h-8 rounded-lg bg-neon-purple/10 flex items-center justify-center text-neon-purple font-bold text-xs">{session.id}</div>
                    <span className={`px-2 py-0.5 text-[9px] bg-${statusColor}-500/20 text-${statusColor}-400 rounded uppercase font-bold`}>{session.state}</span>
                  </div>
                  <h3 className="text-white font-semibold text-sm mb-1 truncate">{session.name}</h3>
                  <p className="text-xs text-slate-500 truncate">{session.error ? session.error : session.pid ? `PID: ${session.pid}` : "..."}</p>
                  
                  <div className="mt-4 flex gap-2">
                     <span className="text-[10px] text-slate-500 font-mono tracking-tighter bg-charcoal-card px-2 py-1 rounded border border-charcoal-border">
                        {session.x},{session.y} ({session.width}x{session.height})
                     </span>
                  </div>
                </div>
              );
            })
          )}
        </div>
      </section>
      {/* END: RuntimeSection */}

       {/* BEGIN: Notice/Footer */}
      {notice && (
        <section className="glass-panel text-sm text-slate-400 rounded-xl p-4 border border-charcoal-border">
          {notice}
        </section>
      )}

      <footer className="mt-auto py-8 text-center" data-purpose="footer-info">
        <div className="flex items-center justify-center gap-6">
          <div className="flex items-center gap-2">
            <div className="w-2 h-2 rounded-full bg-blue-500"></div>
            <span className="text-[10px] text-slate-500 uppercase tracking-widest font-bold">Local Engine</span>
          </div>
          <div className="flex items-center gap-2 text-slate-600 hover:text-slate-400 cursor-pointer transition-colors">
            <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24"><path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"></path></svg>
            <span className="text-[10px] uppercase tracking-widest font-bold">Documentation</span>
          </div>
        </div>
      </footer>
      {/* END: Notice/Footer */}
    </main>
  );
}

export default App;
