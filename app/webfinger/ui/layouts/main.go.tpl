{{define "main"}}
<!DOCTYPE html>

<html lang="en">
  <head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    {{template "meta" .}}
    <title>👉 Webfinger 👈</title>


    <link href="/webfinger/assets/bootstrap.min.css" rel="stylesheet" integrity="sha384-1q8mTJOASx8j1Au+a5WDVnPi2lkFfwwEAa8hDDdjZlpLegxhjVME1fgjWPGmkzs7" crossorigin="anonymous">
    <link href="/webfinger/assets/webfinger.css" rel="stylesheet" crossorigin="anonymous">
  </head>
  <body>
    <nav class="navbar navbar-default">
      <div class="container-fluid">
          <a class="navbar-brand" href="/webfinger">👉 Webfinger 👈</a>
        </div>
      </div>
    </nav>

    <div class=container>
      {{template "content" .}}
    </div>
  </body>
</html>
{{end}}
