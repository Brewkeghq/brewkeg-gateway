export namespace brewkeg {
	
	export class ApplyResult {
	    id: string;
	    label: string;
	    ok: boolean;
	    error?: string;
	    paths?: string[];
	    manual?: string;
	
	    static createFrom(source: any = {}) {
	        return new ApplyResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.ok = source["ok"];
	        this.error = source["error"];
	        this.paths = source["paths"];
	        this.manual = source["manual"];
	    }
	}
	export class BackupEntry {
	    target: string;
	    path: string;
	    existed: boolean;
	    backupFile?: string;
	    transient?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BackupEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.target = source["target"];
	        this.path = source["path"];
	        this.existed = source["existed"];
	        this.backupFile = source["backupFile"];
	        this.transient = source["transient"];
	    }
	}
	export class Backup {
	    id: string;
	    createdAt: string;
	    baseUrl: string;
	    targets: string[];
	    entries: BackupEntry[];
	    notes?: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new Backup(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.createdAt = source["createdAt"];
	        this.baseUrl = source["baseUrl"];
	        this.targets = source["targets"];
	        this.entries = this.convertValues(source["entries"], BackupEntry);
	        this.notes = source["notes"];
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
	
	export class DetectSpec {
	    dirs?: string[];
	    files?: string[];
	    bins?: string[];
	    filesByOS?: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new DetectSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dirs = source["dirs"];
	        this.files = source["files"];
	        this.bins = source["bins"];
	        this.filesByOS = source["filesByOS"];
	    }
	}
	export class EnabledSpec {
	    jsonEnvKeys?: string[];
	    shellBlock?: boolean;
	    tomlTable?: string;
	    rootKey?: string;
	    rootValue?: string;
	    jsonFileKeys?: string[];
	    neverDetectable?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new EnabledSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.jsonEnvKeys = source["jsonEnvKeys"];
	        this.shellBlock = source["shellBlock"];
	        this.tomlTable = source["tomlTable"];
	        this.rootKey = source["rootKey"];
	        this.rootValue = source["rootValue"];
	        this.jsonFileKeys = source["jsonFileKeys"];
	        this.neverDetectable = source["neverDetectable"];
	    }
	}
	export class KVSpec {
	    name: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new KVSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.value = source["value"];
	    }
	}
	export class FileSpec {
	    path: string;
	    paths?: Record<string, string>;
	    kind: string;
	    entries?: KVSpec[];
	    optional?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FileSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.paths = source["paths"];
	        this.kind = source["kind"];
	        this.entries = this.convertValues(source["entries"], KVSpec);
	        this.optional = source["optional"];
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
	
	export class KeyCheck {
	    ok: boolean;
	    valid: boolean;
	    status: number;
	    message: string;
	    latencyMs: number;
	    rejected: boolean;
	    reachable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new KeyCheck(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.valid = source["valid"];
	        this.status = source["status"];
	        this.message = source["message"];
	        this.latencyMs = source["latencyMs"];
	        this.rejected = source["rejected"];
	        this.reachable = source["reachable"];
	    }
	}
	export class PickerOption {
	    model: string;
	    label?: string;
	    description?: string;
	
	    static createFrom(source: any = {}) {
	        return new PickerOption(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.model = source["model"];
	        this.label = source["label"];
	        this.description = source["description"];
	    }
	}
	export class ModelPickerSpec {
	    path: string;
	    replace: boolean;
	    options: PickerOption[];
	
	    static createFrom(source: any = {}) {
	        return new ModelPickerSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.replace = source["replace"];
	        this.options = this.convertValues(source["options"], PickerOption);
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
	
	export class RestartHint {
	    what: string;
	    action: string;
	
	    static createFrom(source: any = {}) {
	        return new RestartHint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.what = source["what"];
	        this.action = source["action"];
	    }
	}
	export class TargetSpec {
	    id: string;
	    label: string;
	    icon?: string;
	    note?: string;
	    detect: DetectSpec;
	    enabled: EnabledSpec;
	    files?: FileSpec[];
	    manual?: string;
	
	    static createFrom(source: any = {}) {
	        return new TargetSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.icon = source["icon"];
	        this.note = source["note"];
	        this.detect = this.convertValues(source["detect"], DetectSpec);
	        this.enabled = this.convertValues(source["enabled"], EnabledSpec);
	        this.files = this.convertValues(source["files"], FileSpec);
	        this.manual = source["manual"];
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
	export class Spec {
	    version: number;
	    targets: TargetSpec[];
	    pickers?: Record<string, ModelPickerSpec>;
	
	    static createFrom(source: any = {}) {
	        return new Spec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.targets = this.convertValues(source["targets"], TargetSpec);
	        this.pickers = this.convertValues(source["pickers"], ModelPickerSpec, true);
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
	
	export class TargetStatus {
	    id: string;
	    label: string;
	    icon?: string;
	    installed: boolean;
	    enabled: boolean;
	    path: string;
	    note?: string;
	
	    static createFrom(source: any = {}) {
	        return new TargetStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.icon = source["icon"];
	        this.installed = source["installed"];
	        this.enabled = source["enabled"];
	        this.path = source["path"];
	        this.note = source["note"];
	    }
	}

}

export namespace main {
	
	export class ConfigureResult {
	    ok: boolean;
	    backupId: string;
	    backupDir: string;
	    results: brewkeg.ApplyResult[];
	    restart?: brewkeg.RestartHint[];
	    message: string;
	    warning?: string;
	    key: brewkeg.KeyCheck;
	
	    static createFrom(source: any = {}) {
	        return new ConfigureResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.backupId = source["backupId"];
	        this.backupDir = source["backupDir"];
	        this.results = this.convertValues(source["results"], brewkeg.ApplyResult);
	        this.restart = this.convertValues(source["restart"], brewkeg.RestartHint);
	        this.message = source["message"];
	        this.warning = source["warning"];
	        this.key = this.convertValues(source["key"], brewkeg.KeyCheck);
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
	export class State {
	    version: string;
	    baseUrl: string;
	    dashboard: string;
	    hasKey: boolean;
	    maskedKey: string;
	    apiKey: string;
	    targets: brewkeg.TargetStatus[];
	    backups: brewkeg.Backup[];
	    platform: string;
	    staleCache?: string;
	    specVersion: number;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.baseUrl = source["baseUrl"];
	        this.dashboard = source["dashboard"];
	        this.hasKey = source["hasKey"];
	        this.maskedKey = source["maskedKey"];
	        this.apiKey = source["apiKey"];
	        this.targets = this.convertValues(source["targets"], brewkeg.TargetStatus);
	        this.backups = this.convertValues(source["backups"], brewkeg.Backup);
	        this.platform = source["platform"];
	        this.staleCache = source["staleCache"];
	        this.specVersion = source["specVersion"];
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
	export class UpdateInfo {
	    available: boolean;
	    current: string;
	    latest: string;
	    url: string;
	    notes?: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.current = source["current"];
	        this.latest = source["latest"];
	        this.url = source["url"];
	        this.notes = source["notes"];
	    }
	}

}

