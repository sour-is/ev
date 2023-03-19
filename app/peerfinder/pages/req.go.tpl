{{template "main" .}}

{{define "meta"}}
  <meta http-equiv="refresh" content="30">
{{end}}


{{define "content"}}
  {{range .Requests}}
    <h2>Results to {{.RequestIP}}{{if .Hidden}} 👁️{{end}}</h2>

    {{range orderByPeer .}}  
      <div class="panel panel-primary" id="peer-{{.Nick}}">
        <div class="panel-heading">
          <b> {{.Country}} :: {{.Name}} :: {{.Nick}} </b>
          <div style='float:right'>
            <a class='btn btn-success' href="#peer-{{.Nick}}">{{ if eq .Latency 0.0 }}&mdash;{{ else }}{{printf "%0.3f ms" .Latency}}{{ end }}</a>
          </div>
        </div>
        <div class="panel-body">
          <b>Note:</b> {{.Note}}<br/>
          <b>VPN Types:</b> {{range .VPNTypes}} {{.}} {{end}}<br/>
          <b>IRC:</b> {{.Nick}}
          <h4>Other Results</h4>

          <table class="table table-striped">
            <thead>
              <tr>
                <th>Peer Name</th>
                <th>Country</th>
                <th>Latency</th>
                <th>Jitter</th>
              </tr>
            </thead>
            <tbody>
              {{range .Results}}
                <tr>
                  <th>{{.Name}}</th>
                  <td>{{.Country}}</td>
                  <td>{{ if eq .Latency 0.0 }}&mdash;{{ else }}{{printf "%0.3f ms" .Latency}}{{ end }}</td>
                  <td>{{ if eq .Jitter 0.0 }}&mdash;{{ else }}{{ printf "%0.3f ms" .Jitter }}{{ end }}</td>
                </tr>
              {{end}}
            </tbody>
          </table>
        </div>
      </div>
    {{end}}
  {{end}}
{{end}}