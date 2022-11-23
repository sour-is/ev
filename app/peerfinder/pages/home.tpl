{{template "main" .}}

{{define "meta"}}
    <meta http-equiv="refresh" content="30">
{{end}}

{{define "content"}}
  <h2>What is this?</h2>

  <p>This tool allows you to find "good" peerings
  for <a href="https://dn42.net">dn42</a>, by measuring the latency from
  various points in the network towards you.</p>

  <p>If you don't know what dn42 is,
  read <a href="https://dn42.net/Home">the website</a> and in particular
  the <a href="https://dn42.net/Getting-started-with-dn42">Getting Started
  guide</a>.</p>

  <h2>How does it work?</h2>

  <p>
    <ol>
      <li>You enter your (Internet) IP address</li>
      <li>Various routers participating in dn42 will ping you over the Internet</li>
      <li>After a short while, you get back all the latency results</li>
      <li>You can then peer with people close to you (low latency)</li>
    </ol>
  </p>

  <form class="form-inline" method="POST" action="/peers/req">
        <label>Ping IP Address [Check Hidden?]:</label>
        <div class="input-group input-group-sm">
            <input class="form-control" type="text" name="req_ip" placeholder="{{ .RemoteIP }}">
            <span class="input-group-addon">
              <input type="checkbox" name="req_hidden" disabled value=1 aria-label="Hidden?">
            </span>
        </div>
        <button class="btn btn-default" type="submit">Submit</button>
  </form>

  <p>If you mark your measurement as hidden, it will not be displayed on the
  page below.  Note that the IP addresses of the target will be shown alongside the result.</p>


  <div class=row>
  <h2>Results</h2>
  {{ with $args := . }}
  {{range $req := .Requests}}
      <div class="panel panel-primary">
          <div class="panel-heading">
            <a href="/peers/req/{{ $req.RequestID }}">{{ $req.RequestIP }} on {{ $req.Created.Format "02 Jan 06 15:04 MST" }}</a> 
            <div style='float:right'><a href="/peers/req/{{ $req.RequestID }}" class='btn btn-success'>{{ countResponses $req }} / {{ $args.CountPeers }} </a></div>
          </div>
          <div class="panel-body"><b>Request ID:</b> {{ $req.RequestID }}</div>
      </div>
  {{end}}
  {{end}}
  </div>

{{end}}
