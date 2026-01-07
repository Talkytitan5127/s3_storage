## Description

You decided to create a competitor to Amazon S3 and know how to make the best file storage service.

A file is sent to server A via REST, it needs to be split into 6 approximately equal parts and saved on storage servers Bn (n ≥ 6).
When a REST request is made to server A, the chunks need to be retrieved from servers Bn, assembled, and the file returned.

Requirements:
1. One server for REST requests
2. Multiple servers for storing file chunks
3. Files can reach a size of 10 GiB

Constraints:
1. Implement a test module for the service that will verify its
functionality and demonstrate file upload and retrieval.
2. Storage servers can be added to the system at any time, but cannot
be removed from the system.
3. Ensure uniform filling of storage servers.
4. Various scenarios must be considered, for example, a user disconnecting
during upload.
5. Storage servers must be separate applications. The communication protocol
between the REST server and storage servers should be chosen independently.
6. Write docker-compose for deploying the service.
