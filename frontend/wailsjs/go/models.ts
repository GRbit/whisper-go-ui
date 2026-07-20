export namespace main {
	
	export class AudioDevice {
	    id: number;
	    name: string;
	    hostApi: string;
	    sampleRate: number;
	    isDefault: boolean;
	    isPulse: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AudioDevice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.hostApi = source["hostApi"];
	        this.sampleRate = source["sampleRate"];
	        this.isDefault = source["isDefault"];
	        this.isPulse = source["isPulse"];
	    }
	}
	export class Config {
	    asrUrl: string;
	    language: string;
	    asrEngine: string;
	    asrTimeout: number;
	    asrRetries: number;
	    hotkey: string;
	    hotkeyMode: string;
	    deviceId: number;
	    debug: boolean;
	    authHeaderName: string;
	    authHeaderValue: string;
	    historyMode: string;
	    theme: string;
	    copyToClipboard: boolean;
	    autoPaste: boolean;
	    pasteCombo: string;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.asrUrl = source["asrUrl"];
	        this.language = source["language"];
	        this.asrEngine = source["asrEngine"];
	        this.asrTimeout = source["asrTimeout"];
	        this.asrRetries = source["asrRetries"];
	        this.hotkey = source["hotkey"];
	        this.hotkeyMode = source["hotkeyMode"];
	        this.deviceId = source["deviceId"];
	        this.debug = source["debug"];
	        this.authHeaderName = source["authHeaderName"];
	        this.authHeaderValue = source["authHeaderValue"];
	        this.historyMode = source["historyMode"];
	        this.theme = source["theme"];
	        this.copyToClipboard = source["copyToClipboard"];
	        this.autoPaste = source["autoPaste"];
	        this.pasteCombo = source["pasteCombo"];
	    }
	}
	export class HistoryEntry {
	    // Go type: time
	    time: any;
	    text: string;
	    durationSec: number;
	
	    static createFrom(source: any = {}) {
	        return new HistoryEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time = this.convertValues(source["time"], null);
	        this.text = source["text"];
	        this.durationSec = source["durationSec"];
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

