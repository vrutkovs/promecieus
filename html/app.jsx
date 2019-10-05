class SearchBar extends React.Component {
  constructor(props) {
    super(props);

    this.handleInputChange = this.handleInputChange.bind(this);
    this.handleSubmit = this.handleSubmit.bind(this);
  }

  handleInputChange(event) {
    this.props.onSearchInput(event.target.value);
  }

  handleSubmit(event) {
    this.props.onSearchSubmit(event);
  }

  render() {
    return (
      <ReactBootstrap.Form horizontal>
        <ReactBootstrap.FormGroup>
          <ReactBootstrap.Col sm={11}>
            <ReactBootstrap.FormControl
              autoFocus="true"
              type="text"
              placeholder="Feed me Prow URLs..."
              value={this.props.searchInput}
              onChange={this.handleInputChange}
            />
          </ReactBootstrap.Col>
          <ReactBootstrap.Col sm={1}>
            <ReactBootstrap.Button
              type="submit"
              onClick={this.handleSubmit}>
              Generate
            </ReactBootstrap.Button>
          </ReactBootstrap.Col>
        </ReactBootstrap.FormGroup>
      </ReactBootstrap.Form>
    );
  }
}

class PlainMessage extends React.Component {
  render() {
    return (
      <div className={this.props.alertClass} role="alert">
        {this.props.message}
      </div>
    )
  }
}

class MessageWithLink extends React.Component {
  render() {
    return (
      <div className="alert alert-success" role="alert">
        <a href={this.props.message}>{this.props.message}</a>
      </div>
    )
  }
}

class Message extends React.Component {
  render() {
    switch (this.props.action) {
      case 'status':
        return <PlainMessage alertClass="alert alert-info" message={this.props.message}/>
        break;
      case 'failure':
        return <PlainMessage alertClass="alert alert-danger" message={this.props.message}/>
        break;
      case 'link':
        return <MessageWithLink message={this.props.message}/>
        break;
      case 'app-label':
        return (
          <ReactBootstrap.Button variant="warning" href={"/delete/" + this.props.message}>
          Delete pods
          </ReactBootstrap.Button>
        )
        break;
    }
  }
}

class Status extends React.Component {
  render() {
    if (this.props.messages.length == 0) {
      return (
        <span></span>
      );
    } else {
      return(
        <div>
          {
            this.props.messages.map(item =>
              <Message action={item.action} message={item.message} />
            )
          }
        </div>
      );
    }
  }
}

class SearchForm extends React.Component {
  constructor(props) {
    super(props);
    this.state = {
      searchInput: '',
      messages: [],
      ws: null
    };

    this.handleSearchInput = this.handleSearchInput.bind(this);
    this.handleSearchSubmit = this.handleSearchSubmit.bind(this);
    this.addMessage = this.addMessage.bind(this);
  }

  handleSearchInput(searchInput) {
    this.setState({searchInput: searchInput});
  }

  handleSearchSubmit(event) {
    event.preventDefault();
    if (this.state.searchInput.length == 0) {
      return;
    }

    try {
      this.state.ws.send(JSON.stringify({
        'action': 'new',
        'message': this.state.searchInput
      }))
    } catch (error) {
      console.log(error)
    }
  }

  addMessage(message) {
    this.setState(state => ({ messages: [...state.messages, message] }))
  }

  componentDidMount() {
    var loc = window.location, new_uri;
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

      this.setState({ ws: ws });

      that.timeout = 250; // reset timer to 250 on open of websocket connection
      clearTimeout(connectInterval); // clear Interval on on open of websocket connection
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
      console.log(evt.data);
      const message = JSON.parse(evt.data);
      this.addMessage(message)
    }
  }

  render() {
    let messages;
    let searchClass;
    if(this.state.messages != null) {
        messages = <Status messages={this.state.messages} />
        searchClass = null
    } else {
        messages = null
        searchClass = 'search-center'
    }
    return (
      <div className={searchClass}>
        <h3>PromeCIeus</h3>
        <SearchBar
          searchInput={this.state.searchInput}
          onSearchInput={this.handleSearchInput}
          onSearchSubmit={this.handleSearchSubmit}
        />
        <br />
        {messages}
      </div>
    );
  }
}

ReactDOM.render(
  <SearchForm />,
  document.getElementById('container')
);
