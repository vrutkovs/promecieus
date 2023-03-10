let storage = new Storage();

class SearchBar extends React.Component {
    constructor(props) {
        super(props);

        this.handleInputChange = this.handleInputChange.bind(this);
        this.handleSnapshotToggle = this.handleSnapshotToggle.bind(this);
        this.handleSubmit = this.handleSubmit.bind(this);
    }

    handleInputChange(event) {
        this.props.onSearchInput(event.target.value);
    }

    handleSnapshotToggle(event) {
        this.props.onSnapshotToggle(event);
    }

    handleSubmit(event) {
        this.props.onSearchSubmit(event);
    }

    render() {
        let btn =
            <ReactBootstrap.Button
                type="submit"
                onClick={this.handleSubmit}
                block>
                Generate
            </ReactBootstrap.Button>
        if (this.props != null && this.props.appName != null) {
            btn =
                <DeleteAppButton
                    onDeleteApp={this.props.onDeleteApp}
                    appName={this.props.appName}
                />
        }
        return (
            <ReactBootstrap.Form horizontal>
                <ReactBootstrap.FormGroup>
                    <ReactBootstrap.Row>
                        <ReactBootstrap.Col xs={9}>
                            <ReactBootstrap.FormControl
                                autoFocus="true"
                                type="text"
                                placeholder="Feed me Prow URLs..."
                                value={this.props.searchInput}
                                onChange={this.handleInputChange}
                            />
                        </ReactBootstrap.Col>
                        <ReactBootstrap.Col xs={3}>
                            <ReactBootstrap.Container>
                                {btn}
                            </ReactBootstrap.Container>
                            <ReactBootstrap.Container>
                                <ReactBootstrap.Form.Check
                                    type="switch"
                                    id="snapshot-switch"
                                    label="2nd Snapshot"
                                    onChange={this.handleSnapshotToggle}
                                />
                            </ReactBootstrap.Container>
                        </ReactBootstrap.Col>
                    </ReactBootstrap.Row>
                </ReactBootstrap.FormGroup>
            </ReactBootstrap.Form>
        );
    }
}

class DeleteAppButton extends React.Component {
    render() {
        return (
            <ReactBootstrap.Button variant="warning" onClick={this.props.onDeleteApp}>
                Delete {this.props.appName}
            </ReactBootstrap.Button>
        )
    }
}

class Message extends React.Component {
    render() {
        var variants = {
            "status": "info",
            "progress": "info",
            "failure": "danger",
            "done": "success"
        }
        switch (this.props.action) {
            case 'done':
            case 'failure':
            case 'status':
                return (
                    <ReactBootstrap.Alert className="alert-small" variant={variants[this.props.action]}>
                        {this.props.message}
                    </ReactBootstrap.Alert>
                )
                break;
            case 'progress':
                return (
                    <ReactBootstrap.Alert className="alert-small" variant={variants[this.props.action]}>
                        <ReactBootstrap.Spinner animation="grow" size="sm"/><span>{this.props.message}</span>
                    </ReactBootstrap.Alert>
                )
                break;
            case 'link':
                return (
                    <ReactBootstrap.Alert className="alert-small" variant="primary">
                        <ReactBootstrap.Alert.Link
                            href={this.props.message}>{this.props.message}</ReactBootstrap.Alert.Link>
                    </ReactBootstrap.Alert>
                )
                break;
            case 'error':
                return (
                    <ReactBootstrap.Alert className="alert-small" variant="danger">
            <pre>
              {this.props.message}
            </pre>
                    </ReactBootstrap.Alert>
                )
                break;
            default:
                return (
                    <span></span>
                )
        }
    }
}

class ResourceQuotaStatus extends React.Component {
    render() {
        if (this.props === null || this.props.resourceQuota === null) {
            return (
                <span></span>
            )
        }
        let used = this.props.resourceQuota.used;
        let hard = this.props.resourceQuota.hard;
        if (typeof used == "undefined" || typeof hard == "undefined") {
            return (
                <span></span>
            )
        }
        return (
            <div>
                <div>Current resource quota</div>
                <ReactBootstrap.ProgressBar now={used} max={hard}
                                            label={used + "/" + hard}/>
            </div>
        )
    }
}

class Status extends React.Component {
    render() {
        if (this.props.messages.length == 0) {
            return (
                <span></span>
            );
        } else {
            return (
                <div>
                    {
                        this.props.messages.map(item =>
                            <Message
                                action={item.action}
                                message={item.message}
                                onDeleteApp={this.props.onDeleteApp}
                            />
                        )
                    }
                </div>
            );
        }
    }
}

class AppsList extends React.Component {
    render() {
        let appCount = Object.keys(this.props.apps).length;
        if (appCount === 0) {
            return false;
        }

        let header = <h4>Currently running Prometheus instances</h4>
        let apps = Object.keys(this.props.apps).map(k => {
            return (
                <ReactBootstrap.Row>
                    <ReactBootstrap.Col xs={2}>
                        <a target="_blank" href={this.props.apps[k]}>{k}</a>
                    </ReactBootstrap.Col>
                    <ReactBootstrap.Col xs={3}>
                        <DeleteAppButton
                            onDeleteApp={() => {
                                this.props.onDeleteApp(k)
                            }}
                            appName={k}
                        />
                    </ReactBootstrap.Col>
                </ReactBootstrap.Row>
            )
        })

        return <div>
            {header}
            {apps}
        </div>
    }
}

class SearchForm extends React.Component {
    constructor(props) {
        super(props);
        this.state = {
            querySearch: '',
            searchInput: '',
            snapshotToggle: false,
            messages: [],
            appName: null,
            apps: storage.getData(),
            ws: null,
            resourceQuota: {
                used: 0,
                hard: 0,
            }
        };

        this.handleSearchInput = this.handleSearchInput.bind(this);
        this.handleSnapshotToggle = this.handleSnapshotToggle.bind(this);
        this.handleSearchSubmit = this.handleSearchSubmit.bind(this);
        this.handleDeleteAppInternal = this.handleDeleteAppInternal.bind(this);
        this.handleDeleteApp = this.handleDeleteApp.bind(this);
        this.handleDeleteCurrentApp = this.handleDeleteCurrentApp.bind(this);
        this.addMessage = this.addMessage.bind(this);
        this.sendWSMessage = this.sendWSMessage.bind(this);
        this.connect = this.connect.bind(this);
        this.check = this.check.bind(this);
        this.search = this.search.bind(this);
    }

    handleSearchInput(searchInput) {
        this.setState({searchInput: searchInput});
    }

    handleSnapshotToggle(event) {
        this.setState({snapshotToggle: !this.state.snapshotToggle})
    }

    handleSearchSubmit(event) {
        event.preventDefault();
        let query = this.state.searchInput;
        if (query.length == 0) {
            return;
        }

        try {
            let url = new URL(query);
            if (this.state.snapshotToggle) {
                url.searchParams.append('altsnap', "true");
            }

            this.search(url.toString());
        } catch (e) {
            console.log(e);
        }
    }

    sendWSMessage(message) {
        // add messages to queue if connection is not ready
        if (!this.state.ws || this.state.ws.readyState != WebSocket.OPEN) {
            if (this.state.ws) {
                console.log("ws.readyState " + this.state.ws.readyState);
            }
            if (!this.ws_msgs) this.ws_msgs = []
            console.log("Added message " + message + " to queue");
            this.ws_msgs.push(message)
        } else {
            console.log("Sending message " + message);
            this.state.ws.send(message)
        }
    }

    search(input) {
        try {
            this.state.messages = [];
            this.sendWSMessage(JSON.stringify({
                'action': 'new',
                'message': input,
            }));
        } catch (error) {
            console.log(error);
        }
    }

    handleDeleteApp(appName) {
        console.log(appName)
        if (this.state.appName === appName) {
            this.handleDeleteCurrentApp()
        } else {
            this.handleDeleteAppInternal(appName)
        }
    }

    handleDeleteCurrentApp() {
        this.handleDeleteAppInternal(this.state.appName)
        // Remove message with app-label from the list
        let newMessages = this.state.messages.slice(1, this.state.messages.length)
        this.setState(state => ({
            messages: newMessages,
            appName: null,
        }))
    }

    handleDeleteAppInternal(appName) {
        try {
            this.sendWSMessage(JSON.stringify({
                'action': 'delete',
                'message': appName
            }))
            storage.removeInstance(appName)
            this.setState(state => ({
                apps: storage.getData(),
            }))
        } catch (error) {
            console.log(error)
        }
    }

    addMessage(message) {
        this.setState(state => ({messages: [...state.messages, message]}))
        if (message.action === "app-label") {
            this.setState(state => ({appName: message.message}))
        }
        if (message.action === "done" || message.action === "error" || message.action === "failure") {
            // Remove message with progress from the list
            let newMessages = this.state.messages.filter(function (message) {
                return message.action != "progress";
            });
            this.setState(state => ({messages: newMessages}))
            if (message.data != null) {
                storage.addInstance(message.data.hash, message.data.url)
            }
            this.setState(state => ({apps: storage.getData()}))
        }
        if (message.action === "rquota") {
            let rquotaStatus = JSON.parse(message.message)
            this.setState(state => ({
                resourceQuota: {
                    used: rquotaStatus.used,
                    hard: rquotaStatus.hard,
                }
            }))
        }
    }

    check() {
        const {ws} = this.state;
        if (!ws || ws.readyState == WebSocket.CLOSED) this.connect(); //check if websocket instance is closed, if so call `connect` function.
    };

    connect() {
        var loc = window.location;
        var ws_uri;
        if (loc.protocol === "https:") {
            ws_uri = "wss:";
        } else {
            ws_uri = "ws:";
        }
        ws_uri += "//" + loc.host;
        ws_uri += "/ws/status";
        var ws = new WebSocket(ws_uri);
        let that = this;
        var connectInterval;

        // websocket onopen event listener
        ws.onopen = () => {
            console.log("websocket connected");
            that.setState({ws: ws});

            that.timeout = 250; // reset timer to 250 on open of websocket connection
            clearTimeout(connectInterval); // clear Interval on on open of websocket connection

            // Send messages if there's a queue
            ws.send(JSON.stringify({"action": "connect"}))
            while (that.ws_msgs && that.ws_msgs.length > 0) {
                ws.send(that.ws_msgs.pop())
            }
        };

        // websocket onclose event listener
        ws.onclose = e => {
            console.log(
                `Socket is closed. Reconnect will be attempted in ${Math.min(
                    10000 / 1000,
                    (that.timeout + that.timeout) / 1000
                )} second.`,
                e.reason
            );

            that.timeout = that.timeout + that.timeout; //increment retry interval
            connectInterval = setTimeout(this.check, Math.min(10000, that.timeout)); //call check function after timeout
        };

        // websocket onerror event listener
        ws.onerror = err => {
            console.error(
                "Socket encountered error: ",
                err.message,
                "Closing socket"
            );

            ws.close();
        };

        ws.onmessage = evt => {
            console.log("Received " + evt.data);
            const message = JSON.parse(evt.data);
            this.addMessage(message)
        }

        this.setState({ws: ws});
    }

    componentDidMount() {
        this.check();
        this.timeout = 0;
        if (!this.state.searchInput) {
            let params = (new URL(window.location)).searchParams;
            let searchInput = params.get('search');
            if (searchInput && searchInput != this.state.querySearch) {
                this.state.querySearch = searchInput;
                this.handleSearchInput(searchInput);
                this.search(searchInput);
            }
        }
    }

    render() {
        let messages;
        let searchClass;
        if (this.state.appName != null) {
            messages =
                <Status messages={this.state.messages}/>
            searchClass = null;
        } else {
            messages = [];
            searchClass = 'search-center';
        }

        return (
            <div className={searchClass}>
                <h3>PromeCIeus</h3>
                <SearchBar
                    searchInput={this.state.searchInput}
                    onSearchInput={this.handleSearchInput}
                    onSearchSubmit={this.handleSearchSubmit}
                    onSnapshotToggle={this.handleSnapshotToggle}
                    onDeleteApp={this.handleDeleteCurrentApp}
                    appName={this.state.appName}
                />
                <ReactBootstrap.Row>
                    <ReactBootstrap.Col xs={4}/>
                    <ReactBootstrap.Col xs={4}>
                        <ResourceQuotaStatus
                            resourceQuota={this.state.resourceQuota || null}
                        />
                    </ReactBootstrap.Col>
                    <ReactBootstrap.Col xs={4}/>
                </ReactBootstrap.Row>
                <br/>
                {messages}
                <AppsList
                    currentApp={this.state.appName}
                    apps={this.state.apps}
                    onDeleteApp={this.handleDeleteApp}
                />
            </div>
        );
    }
}

ReactDOM.render(
    <SearchForm/>,
    document.getElementById('container')
);
