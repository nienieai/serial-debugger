export namespace config {
	
	export class ExternalThemeInfo {
	    id: string;
	    name: any;
	    modes: string[];
	    fallback: string;
	
	    static createFrom(source: any = {}) {
	        return new ExternalThemeInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.modes = source["modes"];
	        this.fallback = source["fallback"];
	    }
	}
	export class ThemeInfo {
	    id: string;
	    _name: any;
	    _modes: string[];
	
	    static createFrom(source: any = {}) {
	        return new ThemeInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this._name = source["_name"];
	        this._modes = source["_modes"];
	    }
	}

}

export namespace main {
	
	export class AppendDefaults {
	    suffix: string;
	
	    static createFrom(source: any = {}) {
	        return new AppendDefaults(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.suffix = source["suffix"];
	    }
	}
	export class AutoSendDefaults {
	    intervalMs: number;
	
	    static createFrom(source: any = {}) {
	        return new AutoSendDefaults(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.intervalMs = source["intervalMs"];
	    }
	}
	export class SerialDefaults {
	    baud: number;
	    dataBits: number;
	    stopBits: string;
	    parity: string;
	    flowControl: string;
	
	    static createFrom(source: any = {}) {
	        return new SerialDefaults(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baud = source["baud"];
	        this.dataBits = source["dataBits"];
	        this.stopBits = source["stopBits"];
	        this.parity = source["parity"];
	        this.flowControl = source["flowControl"];
	    }
	}
	export class AppSettings {
	    displayMode: string;
	    sendRatio: number;
	    quickPanelRatio: number;
	    encoding: string;
	    hexCase: string;
	    hexPrefix: boolean;
	    hexSep: string;
	    crVisible: boolean;
	    hexEscapeMode: string;
	    hexEscapeFormat: string;
	    copyHexEscapes: boolean;
	    displayFontFamily: string;
	    displayFontSize: number;
	    tabSize: number;
	    eolSequence: string;
	    theme: string;
	    colorThemeId: string;
	    iconThemeId: string;
	    language: string;
	    autoCreateSession: boolean;
	    displayColors: string;
	    serial: SerialDefaults;
	    autoSend: AutoSendDefaults;
	    append: AppendDefaults;
	
	    static createFrom(source: any = {}) {
	        return new AppSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.displayMode = source["displayMode"];
	        this.sendRatio = source["sendRatio"];
	        this.quickPanelRatio = source["quickPanelRatio"];
	        this.encoding = source["encoding"];
	        this.hexCase = source["hexCase"];
	        this.hexPrefix = source["hexPrefix"];
	        this.hexSep = source["hexSep"];
	        this.crVisible = source["crVisible"];
	        this.hexEscapeMode = source["hexEscapeMode"];
	        this.hexEscapeFormat = source["hexEscapeFormat"];
	        this.copyHexEscapes = source["copyHexEscapes"];
	        this.displayFontFamily = source["displayFontFamily"];
	        this.displayFontSize = source["displayFontSize"];
	        this.tabSize = source["tabSize"];
	        this.eolSequence = source["eolSequence"];
	        this.theme = source["theme"];
	        this.colorThemeId = source["colorThemeId"];
	        this.iconThemeId = source["iconThemeId"];
	        this.language = source["language"];
	        this.autoCreateSession = source["autoCreateSession"];
	        this.displayColors = source["displayColors"];
	        this.serial = this.convertValues(source["serial"], SerialDefaults);
	        this.autoSend = this.convertValues(source["autoSend"], AutoSendDefaults);
	        this.append = this.convertValues(source["append"], AppendDefaults);
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
	
	
	export class ExternalI18nInfo {
	    filename: string;
	    lang: string;
	    name: string;
	    dir: string;
	
	    static createFrom(source: any = {}) {
	        return new ExternalI18nInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.filename = source["filename"];
	        this.lang = source["lang"];
	        this.name = source["name"];
	        this.dir = source["dir"];
	    }
	}
	export class SerialConfig {
	    port: string;
	    baud: number;
	    dataBits: number;
	    stopBits: string;
	    parity: string;
	
	    static createFrom(source: any = {}) {
	        return new SerialConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.port = source["port"];
	        this.baud = source["baud"];
	        this.dataBits = source["dataBits"];
	        this.stopBits = source["stopBits"];
	        this.parity = source["parity"];
	    }
	}

}

