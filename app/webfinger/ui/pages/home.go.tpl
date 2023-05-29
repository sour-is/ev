{{template "main" .}}

{{define "meta"}}{{end}}

{{define "content"}}
  <form method="GET">
    <div class="input-group">
      <span class="input-group-addon" id="basic-addon1">resource</span>
      <input name="resource" class="form-control" placeholder="acct:..."/>
      <span class="input-group-btn">
        <button class="btn btn-default" type="submit">Go!</button>
      </span>
    </div>
  </form>

  <br/>

  {{ if ne .Err nil }}
  <div class="alert alert-danger" role="alert">
    {{ .Err }}
  </div>
  {{ end }}

  {{ if ne .JRD nil }}
  <div class="panel panel-primary">
    <div class="panel-heading">Webfinger Result</div>

    <table class="table">
      <tr>
        <th style="width:98px">Subject</th>
        <td>
          <div class="media">
            <div class="media-body">
              {{ .JRD.Subject }}
            </div>

            {{ with .JRD.GetLinkByRel "http://webfinger.net/rel/avatar" }}
            {{ if ne . nil }}
              <div class="media-left media-middle">
                <div class="panel panel-default">
                  <div class="panel-body">            
                    <img  src="{{ .HRef }}" />
                  </div>
                </div>
              </div>
            {{ end }}
            {{ end }}

          </div>
        </td>
      </tr>
      
      {{if ne (len .JRD.Aliases) 0}}
      <tr>
        <th>Aliases</th>
        <td>
          <ul class="list-group">
            {{ range .JRD.Aliases }}<li class="list-group-item">{{ . }}</li>
            {{ end }}
          </ul>
        </td>
      </tr>
      {{ end }}
     
      {{ if ne (len .JRD.Properties) 0 }}
      <tr>
        <th>Properties</th>
        <td>
          <div class="list-group truncate">
            {{ range $key, $value := .JRD.Properties }}<div class="list-group-item">
              <h5 class="list-group-item-heading" title="{{ $key }}">{{ propName $key }}</h5>
              <code class="list-group-item-text">{{ $value }}</code>
            </div>
            {{ end }}
          </div>
        </td>
      </tr>
      {{ end }}

      {{ if ne (len .JRD.Links) 0 }}
      {{ range .JRD.Links }}
      <tr class="active">
        {{ if ne (len .Template) 0 }}
        <th> Template </th>
        <td>{{ .Template }}</td>
        {{ else }}
        <th> Link </th>
        <td>{{ if ne (len .HRef) 0 }}<a href="{{ .HRef }}" target="_blank">{{ .HRef }}</a>{{ end }}</td>
        {{ end }}
      <tr>
      <tr> 
        <th> Properties </th> 
        <td>
          <div class="list-group">
            <div class="list-group-item truncate">
              <h5 class="list-group-item-heading">rel<h5>
              <code class="list-group-item-text">{{ .Rel }}</code>
            </div>

            {{ if ne (len .Type) 0 }}<div class="list-group-item truncate">
              <h5 class="list-group-item-heading">type</h5>
              <code class="list-group-item-text">{{ .Type }}</code>
            </div>
            {{ end }}

            {{ range $key, $value := .Properties }}<div class="list-group-item truncate">
              <h5 class="list-group-item-heading" title="{{ $key }}">{{ propName $key }}</h5>
              <code class="list-group-item-text">{{ $value }}</code>
            </div>
            {{ end }}
          </div>
        </td>
      </tr>
      {{ end }}
      {{ end }}

    </table>
  </div>
{{ end }}

  <div class="panel panel-primary">
    <div class="panel-heading">Raw JRD</div>

    <pre style="height: 15em; overflow-y: auto; border: 0px">
Status: {{ .Status }}

{{ .Body | printf "%s" }}
    </pre>
  </div>

{{end}}
