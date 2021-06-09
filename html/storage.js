let localStorageKey = "promecieus"

class Storage {
    constructor() {
    }

    getData() {
        let data = window.localStorage[localStorageKey]
        if (data === undefined || data === null) {
            return {}
        }

        return JSON.parse(data)
    }

    addInstance(hash, url) {
        let data = this.getData()
        data[hash] = url
        window.localStorage[localStorageKey] = JSON.stringify(data)
    }

    removeInstance(hash) {
        let data = this.getData()
        delete data[hash]
        window.localStorage[localStorageKey] = JSON.stringify(data)
    }
}