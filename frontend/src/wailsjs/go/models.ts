export namespace dsp {
	
	export class Param {
	    id: string;
	    label: string;
	    min: number;
	    max: number;
	    step: number;
	    default: number;
	
	    static createFrom(source: any = {}) {
	        return new Param(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.min = source["min"];
	        this.max = source["max"];
	        this.step = source["step"];
	        this.default = source["default"];
	    }
	}
	export class Chain {
	    id: string;
	    name: string;
	    note: string;
	    params: Param[];
	    voice?: boolean;
	    key?: string;
	    pedal?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Chain(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.note = source["note"];
	        this.params = this.convertValues(source["params"], Param);
	        this.voice = source["voice"];
	        this.key = source["key"];
	        this.pedal = source["pedal"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class EnvPoint {
	    t: number;
	    db: number;
	
	    static createFrom(source: any = {}) {
	        return new EnvPoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.t = source["t"];
	        this.db = source["db"];
	    }
	}
	
	export class Step {
	    chain: string;
	    params?: Record<string, number>;
	    off?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Step(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.chain = source["chain"];
	        this.params = source["params"];
	        this.off = source["off"];
	    }
	}
	export class Preset {
	    id: string;
	    name: string;
	    note: string;
	    steps: Step[];
	
	    static createFrom(source: any = {}) {
	        return new Preset(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.note = source["note"];
	        this.steps = this.convertValues(source["steps"], Step);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class YuePlayerState {
	    playing: boolean;
	    position_sec: number;
	    duration_sec: number;
	    job_id: number;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new YuePlayerState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.playing = source["playing"];
	        this.position_sec = source["position_sec"];
	        this.duration_sec = source["duration_sec"];
	        this.job_id = source["job_id"];
	        this.error = source["error"];
	    }
	}

}

export namespace studio {
	
	export class InsertReport {
	    child_id: number;
	    start_sec: number;
	    aligned: boolean;
	    score: number;
	    gain: number;
	
	    static createFrom(source: any = {}) {
	        return new InsertReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.child_id = source["child_id"];
	        this.start_sec = source["start_sec"];
	        this.aligned = source["aligned"];
	        this.score = source["score"];
	        this.gain = source["gain"];
	    }
	}
	export class PreviewResult {
	    wet: string;
	    dry: string;
	    wet_solo?: string;
	    dry_solo?: string;
	    from: number;
	    to: number;
	    dur_sec: number;
	
	    static createFrom(source: any = {}) {
	        return new PreviewResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.wet = source["wet"];
	        this.dry = source["dry"];
	        this.wet_solo = source["wet_solo"];
	        this.dry_solo = source["dry_solo"];
	        this.from = source["from"];
	        this.to = source["to"];
	        this.dur_sec = source["dur_sec"];
	    }
	}
	export class RebuildResult {
	    variant?: yue.DspVariant;
	    inserts: InsertReport[];
	
	    static createFrom(source: any = {}) {
	        return new RebuildResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.variant = this.convertValues(source["variant"], yue.DspVariant);
	        this.inserts = this.convertValues(source["inserts"], InsertReport);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SectionSpec {
	    child_id: number;
	    from: number;
	    to: number;
	    lead: number;
	    beat_sec: number;
	    stems: string[];
	    db: number;
	    fade_in: number;
	    fade_out: number;
	    revoice: boolean;
	    keep_high_hz: number;
	    chain?: string;
	    params?: Record<string, number>;
	    steps?: dsp.Step[];
	    envelope?: dsp.EnvPoint[];
	
	    static createFrom(source: any = {}) {
	        return new SectionSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.child_id = source["child_id"];
	        this.from = source["from"];
	        this.to = source["to"];
	        this.lead = source["lead"];
	        this.beat_sec = source["beat_sec"];
	        this.stems = source["stems"];
	        this.db = source["db"];
	        this.fade_in = source["fade_in"];
	        this.fade_out = source["fade_out"];
	        this.revoice = source["revoice"];
	        this.keep_high_hz = source["keep_high_hz"];
	        this.chain = source["chain"];
	        this.params = source["params"];
	        this.steps = this.convertValues(source["steps"], dsp.Step);
	        this.envelope = this.convertValues(source["envelope"], dsp.EnvPoint);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SplicePart {
	    job_id: number;
	    from: number;
	    to: number;
	    gain_db: number;
	
	    static createFrom(source: any = {}) {
	        return new SplicePart(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.job_id = source["job_id"];
	        this.from = source["from"];
	        this.to = source["to"];
	        this.gain_db = source["gain_db"];
	    }
	}

}

export namespace yue {
	
	export class BeatGrid {
	    bpm: number;
	    offset: number;
	    strength: number;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new BeatGrid(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.bpm = source["bpm"];
	        this.offset = source["offset"];
	        this.strength = source["strength"];
	        this.source = source["source"];
	    }
	}
	export class ContourBar {
	    index: number;
	    start: number;
	    end: number;
	    notes: string[];
	
	    static createFrom(source: any = {}) {
	        return new ContourBar(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.start = source["start"];
	        this.end = source["end"];
	        this.notes = source["notes"];
	    }
	}
	export class CopilotParams {
	    theme: string;
	    style: string;
	    example: string;
	    lang: string;
	    instruction?: string;
	
	    static createFrom(source: any = {}) {
	        return new CopilotParams(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.theme = source["theme"];
	        this.style = source["style"];
	        this.example = source["example"];
	        this.lang = source["lang"];
	        this.instruction = source["instruction"];
	    }
	}
	export class CopilotResult {
	    text: string;
	    seconds: number;
	
	    static createFrom(source: any = {}) {
	        return new CopilotResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.seconds = source["seconds"];
	    }
	}
	export class Corpus {
	    id: number;
	    name: string;
	    status: string;
	    created_at: string;
	    tracks: number;
	    has_profile: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Corpus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.status = source["status"];
	        this.created_at = source["created_at"];
	        this.tracks = source["tracks"];
	        this.has_profile = source["has_profile"];
	    }
	}
	export class DspVariant {
	    file: string;
	    created_at: string;
	    metrics: Record<string, any>;
	    label?: string;
	
	    static createFrom(source: any = {}) {
	        return new DspVariant(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.file = source["file"];
	        this.created_at = source["created_at"];
	        this.metrics = source["metrics"];
	        this.label = source["label"];
	    }
	}
	export class FxRequest {
	    source: string;
	    chain: any[];
	    from?: number;
	    to?: number;
	    output?: string;
	    label?: string;
	    preview?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FxRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.chain = source["chain"];
	        this.from = source["from"];
	        this.to = source["to"];
	        this.output = source["output"];
	        this.label = source["label"];
	        this.preview = source["preview"];
	    }
	}
	export class HealthInfo {
	    status: string;
	    model_loaded: boolean;
	    load_error: string;
	
	    static createFrom(source: any = {}) {
	        return new HealthInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.model_loaded = source["model_loaded"];
	        this.load_error = source["load_error"];
	    }
	}
	export class Job {
	    id: number;
	    title: string;
	    status: string;
	    style: string;
	    lyrics: string;
	    seed: number;
	    cot: string;
	    req_abc: string;
	    error: string;
	    duration_sec: number;
	    audio_file: string;
	    mp3_file: string;
	    wav_file: string;
	    abc_file: string;
	    created_at: string;
	    finished_at: string;
	    draft?: boolean;
	    parent_id?: number;
	    role?: string;
	    overdub_of?: number;
	    head_id?: number;
	    folder?: string;
	    vocal_leak?: string;
	    mixes?: number;
	    voice_src?: number;
	    temperature?: number;
	    cfg?: number;
	    stage?: string;
	    tokens?: number;
	    tok_per_s?: number;
	    elapsed_s?: number;
	    progress_pct?: number;
	
	    static createFrom(source: any = {}) {
	        return new Job(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.status = source["status"];
	        this.style = source["style"];
	        this.lyrics = source["lyrics"];
	        this.seed = source["seed"];
	        this.cot = source["cot"];
	        this.req_abc = source["req_abc"];
	        this.error = source["error"];
	        this.duration_sec = source["duration_sec"];
	        this.audio_file = source["audio_file"];
	        this.mp3_file = source["mp3_file"];
	        this.wav_file = source["wav_file"];
	        this.abc_file = source["abc_file"];
	        this.created_at = source["created_at"];
	        this.finished_at = source["finished_at"];
	        this.draft = source["draft"];
	        this.parent_id = source["parent_id"];
	        this.role = source["role"];
	        this.overdub_of = source["overdub_of"];
	        this.head_id = source["head_id"];
	        this.folder = source["folder"];
	        this.vocal_leak = source["vocal_leak"];
	        this.mixes = source["mixes"];
	        this.voice_src = source["voice_src"];
	        this.temperature = source["temperature"];
	        this.cfg = source["cfg"];
	        this.stage = source["stage"];
	        this.tokens = source["tokens"];
	        this.tok_per_s = source["tok_per_s"];
	        this.elapsed_s = source["elapsed_s"];
	        this.progress_pct = source["progress_pct"];
	    }
	}
	export class LyricsResult {
	    text: string;
	    seconds: number;
	
	    static createFrom(source: any = {}) {
	        return new LyricsResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.seconds = source["seconds"];
	    }
	}
	export class PlanCeiling {
	    top: string;
	    ceiling: string;
	    new_top: string;
	
	    static createFrom(source: any = {}) {
	        return new PlanCeiling(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.top = source["top"];
	        this.ceiling = source["ceiling"];
	        this.new_top = source["new_top"];
	    }
	}
	export class PlanChange {
	    voice: string;
	    bar: number;
	    start: number;
	    end: number;
	    before: string;
	    after: string;
	
	    static createFrom(source: any = {}) {
	        return new PlanChange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.voice = source["voice"];
	        this.bar = source["bar"];
	        this.start = source["start"];
	        this.end = source["end"];
	        this.before = source["before"];
	        this.after = source["after"];
	    }
	}
	export class PlanCheck {
	    bars: Record<string, Array<number>>;
	    duration: number[];
	    changed: PlanChange[];
	    changed_total: number;
	    ceiling: PlanCeiling;
	    warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new PlanCheck(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.bars = source["bars"];
	        this.duration = source["duration"];
	        this.changed = this.convertValues(source["changed"], PlanChange);
	        this.changed_total = source["changed_total"];
	        this.ceiling = this.convertValues(source["ceiling"], PlanCeiling);
	        this.warnings = source["warnings"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PlanParams {
	    style: string;
	    lyrics: string;
	    seed: number;
	    cot: string;
	
	    static createFrom(source: any = {}) {
	        return new PlanParams(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.style = source["style"];
	        this.lyrics = source["lyrics"];
	        this.seed = source["seed"];
	        this.cot = source["cot"];
	    }
	}
	export class PlanResult {
	    abc: string;
	    truncated: boolean;
	    seconds: number;
	    seed?: number;
	
	    static createFrom(source: any = {}) {
	        return new PlanResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.abc = source["abc"];
	        this.truncated = source["truncated"];
	        this.seconds = source["seconds"];
	        this.seed = source["seed"];
	    }
	}
	export class Reference {
	    id: string;
	    created_at: string;
	    size: number;
	    metrics: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new Reference(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.created_at = source["created_at"];
	        this.size = source["size"];
	        this.metrics = source["metrics"];
	    }
	}
	export class StatsRunning {
	    job_id: number;
	    title: string;
	    stage: string;
	    progress_pct?: number;
	    tok_per_s: number;
	
	    static createFrom(source: any = {}) {
	        return new StatsRunning(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.job_id = source["job_id"];
	        this.title = source["title"];
	        this.stage = source["stage"];
	        this.progress_pct = source["progress_pct"];
	        this.tok_per_s = source["tok_per_s"];
	    }
	}
	export class StatsInfo {
	    model_loaded: boolean;
	    vram_total?: number;
	    vram_used?: number;
	    queue: Record<string, number>;
	    running?: StatsRunning;
	    gpu_waiting: number;
	
	    static createFrom(source: any = {}) {
	        return new StatsInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.model_loaded = source["model_loaded"];
	        this.vram_total = source["vram_total"];
	        this.vram_used = source["vram_used"];
	        this.queue = source["queue"];
	        this.running = this.convertValues(source["running"], StatsRunning);
	        this.gpu_waiting = source["gpu_waiting"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class SubmitParams {
	    title: string;
	    style: string;
	    lyrics: string;
	    seed: number;
	    cot: string;
	    abc?: string;
	    draft?: boolean;
	    arc?: string;
	    max_tokens?: number;
	    parent_id?: number;
	    role?: string;
	    temperature?: number;
	    cfg?: number;
	
	    static createFrom(source: any = {}) {
	        return new SubmitParams(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.style = source["style"];
	        this.lyrics = source["lyrics"];
	        this.seed = source["seed"];
	        this.cot = source["cot"];
	        this.abc = source["abc"];
	        this.draft = source["draft"];
	        this.arc = source["arc"];
	        this.max_tokens = source["max_tokens"];
	        this.parent_id = source["parent_id"];
	        this.role = source["role"];
	        this.temperature = source["temperature"];
	        this.cfg = source["cfg"];
	    }
	}
	export class Tone {
	    hz: number;
	    prominence_db: number;
	
	    static createFrom(source: any = {}) {
	        return new Tone(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hz = source["hz"];
	        this.prominence_db = source["prominence_db"];
	    }
	}
	export class TranscribeResult {
	    id: string;
	    abc: string;
	    seconds: number;
	    warnings: string[];
	    stats: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new TranscribeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.abc = source["abc"];
	        this.seconds = source["seconds"];
	        this.warnings = source["warnings"];
	        this.stats = source["stats"];
	    }
	}
	export class TranslateResult {
	    text: string;
	    seconds: number;
	
	    static createFrom(source: any = {}) {
	        return new TranslateResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.seconds = source["seconds"];
	    }
	}
	export class VocalContour {
	    bars: ContourBar[];
	    median_hz: number;
	    low_hz: number;
	    high_hz: number;
	
	    static createFrom(source: any = {}) {
	        return new VocalContour(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.bars = this.convertValues(source["bars"], ContourBar);
	        this.median_hz = source["median_hz"];
	        this.low_hz = source["low_hz"];
	        this.high_hz = source["high_hz"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Voice {
	    id: number;
	    name: string;
	    job_id: number;
	    params: string;
	    seed: number;
	    created_at: string;
	    job_alive: boolean;
	    has_audio: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Voice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.job_id = source["job_id"];
	        this.params = source["params"];
	        this.seed = source["seed"];
	        this.created_at = source["created_at"];
	        this.job_alive = source["job_alive"];
	        this.has_audio = source["has_audio"];
	    }
	}
	export class VoiceParams {
	    ref_job_id: number;
	    ref_from?: number;
	    ref_dur?: number;
	    steps?: number;
	    title?: string;
	
	    static createFrom(source: any = {}) {
	        return new VoiceParams(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ref_job_id = source["ref_job_id"];
	        this.ref_from = source["ref_from"];
	        this.ref_dur = source["ref_dur"];
	        this.steps = source["steps"];
	        this.title = source["title"];
	    }
	}

}

