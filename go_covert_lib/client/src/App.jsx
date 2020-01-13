import React, { useState, useEffect } from 'react';
import Button from 'react-bootstrap/Button';
import Dropdown from 'react-bootstrap/Dropdown';
import FormControl from 'react-bootstrap/FormControl';
import Spinner from 'react-bootstrap/Spinner';

import IPInput from './ui-components/IPInput';
import PortInput from './ui-components/PortInput';
import './styles.css';

/**
 * IMPORTANT NOTE: For styling, refer to https://getbootstrap.com/docs/4.0/utilities/position/
 */
const App = () => {
  const [textToSend, setTextToSend] = useState('');
  const [processors, setProcessors] = useState([]);
  const [processor, setProcessor] = useState({});
  const [channels, setChannels] = useState([]);
  const [channel, setChannel] = useState({});
  const [config, setConfig] = useState({});
  const [isLoading, setLoading] = useState(true);

  const sendConfig = (ws) => {
    const cmd = JSON.stringify({ OpCode: 'config' });
    ws.send(cmd);
  };

  const handleMessage = (msg) => {
    switch (msg.OpCode) {
      case 'config':
        setChannels(msg.Default.Channel);
        setProcessors(msg.Default.Processor);
        setLoading(false);
        break;
      default:
          // TODO:
    }
  };

  useEffect(() => {
    // Matches just the "127.0.0.1:8080" portion of the address
    const addressRegex = /[a-zA-Z0-9.]+:[\d]+/g;
    const ws = new WebSocket(`ws://${window.location.href.match(addressRegex)[0]}/api/ws`);
    // TODO: The line below exists for easy personal debugging
    // const ws = new WebSocket('ws://localhost:8080/api/ws');
    ws.binaryType = 'arraybuffer';
    ws.onopen = _e => sendConfig(ws);
    ws.onerror = _e => console.log('UNIMPLEMENTED'); // TODO:
    ws.onmessage = e => handleMessage(JSON.parse(e.data));
  }, []);

  console.log("### config", config);

  return isLoading ? (
    <div className="spinner-container">
      <Spinner animation="border" role="status" />
    </div>
  ) : (
    <div className="m-2">
      <h2 className="m-1">Messaging</h2>
      <FormControl
        as="textarea"
        className="w-25 m-1"
        value={textToSend}
        onChange={e => setTextToSend(e.target.value)}
      />
      <Button variant="primary" className="m-1">Send Message</Button>
      <br />
      <div className="m-1">Incoming Messages</div>
      <FormControl
        as="textarea"
        className="w-25 m-1"
        readOnly
      />
      <h2 className="m-1 mt-5">Configuration</h2>
      <Dropdown className="m-1">
        <Dropdown.Toggle
          className="w-25"
          variant="outline-primary"
        >
          {channel.value || 'Select a Channel'}
        </Dropdown.Toggle>
        <Dropdown.Menu className="w-25">
          {
            Object.keys(channels).map(chan => (
              <Dropdown.Item
                as="option"
                active={chan === channel.value}
                onClick={(e) => {
                  setChannel({
                    value: e.target.value,
                    properties: channels[chan],
                  });
                  setConfig(channels[chan]);
                }}
                value={chan}
                key={chan}
              >
                {chan}
              </Dropdown.Item>
            ))
          }
        </Dropdown.Menu>
      </Dropdown>
      {Object.keys(config).map((key) => {
        const opt = config[key];
        // TODO: Grouping logic
        switch (opt.Type) {
          case 'ipv4':
            return (
              <IPInput
                label={opt.Display.Name}
                default="127.0.0.1"
                key={key}
              />
            );
          case 'u16':
            return (
              <PortInput
                label={opt.Display.Name}
                default="127.0.0.1"
                key={key}
              />
            );
          default:
            return (<div key={key}>UNIMPLEMENTED</div>);
        }
      })}
      <Button variant="success" className="m-1 w-25">Open Covert Channel</Button>
      <Button variant="danger" className="m-1 w-25">Close Covert Channel</Button>
    </div>
  );
};

export default App;