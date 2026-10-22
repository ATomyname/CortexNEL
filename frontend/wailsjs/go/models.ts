export namespace app {
	
	export class AccountDTO {
	    id: string;
	    label: string;
	    channel: string;
	    userId: number;
	    roleName: string;
	    tokenPersisted: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AccountDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.channel = source["channel"];
	        this.userId = source["userId"];
	        this.roleName = source["roleName"];
	        this.tokenPersisted = source["tokenPersisted"];
	    }
	}
	export class EnvDTO {
	    id: string;
	    name: string;
	    clientDir: string;
	    modsDir: string;
	    javaPath: string;
	    jvmArgs: string[];
	    launchArgs: string[];
	
	    static createFrom(source: any = {}) {
	        return new EnvDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.clientDir = source["clientDir"];
	        this.modsDir = source["modsDir"];
	        this.javaPath = source["javaPath"];
	        this.jvmArgs = source["jvmArgs"];
	        this.launchArgs = source["launchArgs"];
	    }
	}
	export class GenerateRoleNameOptions {
	    prefix?: string;
	    suffix?: string;
	    style?: string;
	    baseWord?: string;
	    digits?: number;
	    maxLength?: number;
	    useHomophone?: boolean;
	    exactTone?: boolean;
	    allowSelf?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GenerateRoleNameOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.prefix = source["prefix"];
	        this.suffix = source["suffix"];
	        this.style = source["style"];
	        this.baseWord = source["baseWord"];
	        this.digits = source["digits"];
	        this.maxLength = source["maxLength"];
	        this.useHomophone = source["useHomophone"];
	        this.exactTone = source["exactTone"];
	        this.allowSelf = source["allowSelf"];
	    }
	}
	export class NetworkAddressDTO {
	    host: string;
	    port: number;
	
	    static createFrom(source: any = {}) {
	        return new NetworkAddressDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.port = source["port"];
	    }
	}
	export class NetworkServerDTO {
	    entityId: string;
	    name: string;
	    onlineCount: string;
	    briefSummary: string;
	    imageUrl: string;
	    mcVersion?: string;
	    isBedrockOnly: boolean;
	    isMaintaining: boolean;
	
	    static createFrom(source: any = {}) {
	        return new NetworkServerDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entityId = source["entityId"];
	        this.name = source["name"];
	        this.onlineCount = source["onlineCount"];
	        this.briefSummary = source["briefSummary"];
	        this.imageUrl = source["imageUrl"];
	        this.mcVersion = source["mcVersion"];
	        this.isBedrockOnly = source["isBedrockOnly"];
	        this.isMaintaining = source["isMaintaining"];
	    }
	}
	export class NetworkServerDetailDTO {
	    entityId: string;
	    name: string;
	    imageUrl: string;
	    description: string;
	    mcVersion?: string;
	    mcVersionList?: string[];
	    isBedrockOnly: boolean;
	    isMaintaining: boolean;
	
	    static createFrom(source: any = {}) {
	        return new NetworkServerDetailDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entityId = source["entityId"];
	        this.name = source["name"];
	        this.imageUrl = source["imageUrl"];
	        this.description = source["description"];
	        this.mcVersion = source["mcVersion"];
	        this.mcVersionList = source["mcVersionList"];
	        this.isBedrockOnly = source["isBedrockOnly"];
	        this.isMaintaining = source["isMaintaining"];
	    }
	}
	export class PluginDTO {
	    fileName: string;
	    name: string;
	    iid: string;
	    md5: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PluginDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fileName = source["fileName"];
	        this.name = source["name"];
	        this.iid = source["iid"];
	        this.md5 = source["md5"];
	        this.enabled = source["enabled"];
	    }
	}
	export class RentalServerDTO {
	    entityId: string;
	    name: string;
	    serverName: string;
	    hasPassword: string;
	    mcVersion: string;
	    playerCount: number;
	    capacity: number;
	    imageUrl: string;
	    briefSummary: string;
	
	    static createFrom(source: any = {}) {
	        return new RentalServerDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entityId = source["entityId"];
	        this.name = source["name"];
	        this.serverName = source["serverName"];
	        this.hasPassword = source["hasPassword"];
	        this.mcVersion = source["mcVersion"];
	        this.playerCount = source["playerCount"];
	        this.capacity = source["capacity"];
	        this.imageUrl = source["imageUrl"];
	        this.briefSummary = source["briefSummary"];
	    }
	}
	export class RoleDTO {
	    id: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new RoleDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	    }
	}
	export class ServerDTO {
	    id: string;
	    name: string;
	    gameId: string;
	    host: string;
	    port: number;
	    mcVersion?: string;
	    isRental?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ServerDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.gameId = source["gameId"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.mcVersion = source["mcVersion"];
	        this.isRental = source["isRental"];
	    }
	}
	export class SessionInfoDTO {
	    sessionId: string;
	    serverId: string;
	    accountId: string;
	    roleName?: string;
	    listenAddr: string;
	    stage: string;
	    launchMirror: boolean;
	    startedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new SessionInfoDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.serverId = source["serverId"];
	        this.accountId = source["accountId"];
	        this.roleName = source["roleName"];
	        this.listenAddr = source["listenAddr"];
	        this.stage = source["stage"];
	        this.launchMirror = source["launchMirror"];
	        this.startedAt = source["startedAt"];
	    }
	}
	export class SettingsDTO {
	    mainAccountId: string;
	    mainEnvId: string;
	    mainServerId: string;
	    credentialPersist: boolean;
	    hideZeroPlayers: boolean;
	    hideMaintaining: boolean;
	    hideBedrockOnly: boolean;
	    allowLan: boolean;
	    lanPort?: number;
	    roleNamePrefix?: string;
	    roleNameSuffix?: string;
	    roleNameStyle?: string;
	    roleNameBaseWord?: string;
	    roleNameDigits: number;
	    roleNameHomophone?: boolean;
	    roleNameExactTone?: boolean;
	    roleNameAllowSelf?: boolean;
	    clientDataDir?: string;
	    bypassSystemProxy?: boolean;
	    shadowEnabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SettingsDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mainAccountId = source["mainAccountId"];
	        this.mainEnvId = source["mainEnvId"];
	        this.mainServerId = source["mainServerId"];
	        this.credentialPersist = source["credentialPersist"];
	        this.hideZeroPlayers = source["hideZeroPlayers"];
	        this.hideMaintaining = source["hideMaintaining"];
	        this.hideBedrockOnly = source["hideBedrockOnly"];
	        this.allowLan = source["allowLan"];
	        this.lanPort = source["lanPort"];
	        this.roleNamePrefix = source["roleNamePrefix"];
	        this.roleNameSuffix = source["roleNameSuffix"];
	        this.roleNameStyle = source["roleNameStyle"];
	        this.roleNameBaseWord = source["roleNameBaseWord"];
	        this.roleNameDigits = source["roleNameDigits"];
	        this.roleNameHomophone = source["roleNameHomophone"];
	        this.roleNameExactTone = source["roleNameExactTone"];
	        this.roleNameAllowSelf = source["roleNameAllowSelf"];
	        this.clientDataDir = source["clientDataDir"];
	        this.bypassSystemProxy = source["bypassSystemProxy"];
	        this.shadowEnabled = source["shadowEnabled"];
	    }
	}
	export class ShadowGateStatsDTO {
	    swallowedA: number;
	    swallowedB: number;
	    fallbacks: number;
	    blockedSB: number;
	    blockedCB: number;
	
	    static createFrom(source: any = {}) {
	        return new ShadowGateStatsDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.swallowedA = source["swallowedA"];
	        this.swallowedB = source["swallowedB"];
	        this.fallbacks = source["fallbacks"];
	        this.blockedSB = source["blockedSB"];
	        this.blockedCB = source["blockedCB"];
	    }
	}
	export class ShadowStatusDTO {
	    enabled: boolean;
	    listening: boolean;
	    addr: string;
	    stage: string;
	    sender: string;
	    gateStats: ShadowGateStatsDTO;
	    recorderFrames: number;
	    recorderBytes: number;
	    recorderReady: boolean;
	    truncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ShadowStatusDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.listening = source["listening"];
	        this.addr = source["addr"];
	        this.stage = source["stage"];
	        this.sender = source["sender"];
	        this.gateStats = this.convertValues(source["gateStats"], ShadowGateStatsDTO);
	        this.recorderFrames = source["recorderFrames"];
	        this.recorderBytes = source["recorderBytes"];
	        this.recorderReady = source["recorderReady"];
	        this.truncated = source["truncated"];
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
	export class SkinDTO {
	    entityId: string;
	    name: string;
	    previewUrl: string;
	
	    static createFrom(source: any = {}) {
	        return new SkinDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entityId = source["entityId"];
	        this.name = source["name"];
	        this.previewUrl = source["previewUrl"];
	    }
	}
	export class StartSessionResult {
	    sessionId: string;
	    listenAddr: string;
	    localAddr?: string;
	    lanAddrs?: string[];
	    primaryLan?: string;
	    isLan?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new StartSessionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.listenAddr = source["listenAddr"];
	        this.localAddr = source["localAddr"];
	        this.lanAddrs = source["lanAddrs"];
	        this.primaryLan = source["primaryLan"];
	        this.isLan = source["isLan"];
	    }
	}

}

