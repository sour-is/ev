{{define "main"}}
<!DOCTYPE html>

<html>
<head>
    <meta charset="UTF-8">
	{{template "meta" .}}
    <title>DN42 PingFinder</title>

    <link href="/peers/assets/bootstrap.min.css" rel="stylesheet" integrity="sha384-1q8mTJOASx8j1Au+a5WDVnPi2lkFfwwEAa8hDDdjZlpLegxhjVME1fgjWPGmkzs7" crossorigin="anonymous">
    <link href="/peers/assets/peerfinder.css" rel="stylesheet" crossorigin="anonymous">
</head>

<body>

<div class="container-fluid">
      <div class="header clearfix">
        <nav>
          <ul class="nav nav-pills pull-right">
            <li role="presentation"><a href="/peers">Home</a></li>
            <!--
              <li role="presentation"><a href="/peers/status">Status</a></li>
            -->
            <li role="presentation"><a href="//util.sour.is/peer">Sign up/Manage</a></li>
            <li role="presentation"><a href="https://git.dn42.dev/dn42/pingfinder/src/branch/master/clients">Scripts</a></li>
          </ul>
        </nav>
        <h3 class="text-muted">DN42 PeerFinder</h3>
      </div>
</div>

<div class=container>
	{{template "content" .}}
</div>

</body>
</html>
{{end}}
