export namespace main {
	
	export class APIStatus {
	    running: boolean;
	    port: number;
	    url: string;
	    tls: boolean;
	    fingerprint: string;
	
	    static createFrom(source: any = {}) {
	        return new APIStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.port = source["port"];
	        this.url = source["url"];
	        this.tls = source["tls"];
	        this.fingerprint = source["fingerprint"];
	    }
	}
	export class FileResult {
	    path: string;
	    name: string;
	    content: string;
	    canceled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FileResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.name = source["name"];
	        this.content = source["content"];
	        this.canceled = source["canceled"];
	    }
	}

}

export namespace notes {
	
	export class Stats {
	    lines: number;
	    words: number;
	    chars: number;
	    charsNoSpaces: number;
	
	    static createFrom(source: any = {}) {
	        return new Stats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.lines = source["lines"];
	        this.words = source["words"];
	        this.chars = source["chars"];
	        this.charsNoSpaces = source["charsNoSpaces"];
	    }
	}

}

export namespace settings {
	
	export class Settings {
	    theme: string;
	    opacity: number;
	    tabPosition: string;
	    wordWrap: boolean;
	    fontFamily: string;
	    fontSize: number;
	    apiAutoStart: boolean;
	    apiPort: number;
	    apiKey: string;
	    apiAllowlist: string[];
	    apiHttps: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.theme = source["theme"];
	        this.opacity = source["opacity"];
	        this.tabPosition = source["tabPosition"];
	        this.wordWrap = source["wordWrap"];
	        this.fontFamily = source["fontFamily"];
	        this.fontSize = source["fontSize"];
	        this.apiAutoStart = source["apiAutoStart"];
	        this.apiPort = source["apiPort"];
	        this.apiKey = source["apiKey"];
	        this.apiAllowlist = source["apiAllowlist"];
	        this.apiHttps = source["apiHttps"];
	    }
	}

}

