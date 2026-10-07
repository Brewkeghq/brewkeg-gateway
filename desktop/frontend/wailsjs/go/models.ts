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
	
	    static createFrom(source: any = {}) {
	        return new BackupEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.target = source["target"];
	        this.path = source["path"];
	        this.existed = source["existed"];
	        this.backupFile = source["backupFile"];
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
	
	export class TargetStatus {
	    id: string;
	    label: string;
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
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new ConfigureResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.backupId = source["backupId"];
	        this.backupDir = source["backupDir"];
	        this.results = this.convertValues(source["results"], brewkeg.ApplyResult);
	        this.message = source["message"];
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
	    targets: brewkeg.TargetStatus[];
	    backups: brewkeg.Backup[];
	    platform: string;
	
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
	        this.targets = this.convertValues(source["targets"], brewkeg.TargetStatus);
	        this.backups = this.convertValues(source["backups"], brewkeg.Backup);
	        this.platform = source["platform"];
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

